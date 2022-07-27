package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/disintegration/imaging"
	"github.com/gorilla/mux"
	log "github.com/sirupsen/logrus"
	image2 "image"
	"image/jpeg"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

var ErrEnvVarEmpty = errors.New("getenv: environment variable empty")

var (
	httpPort, imageDir string
	tasks              map[int]Task
	syncMapMutex              = sync.RWMutex{}
	hasActiveProxy            = false
	proxyApiServer            = ""
	activeProxy               = &Proxy{}
	proxyLimit                = 20
	taskTTL            uint32 = 20
	checkerOnly               = false
	proxyGroup                = ""
)

type Images struct {
	Images []Image `json:"images"`
}

type Image struct {
	Url  string `json:"image"`
	Crop bool   `json:"crop"`
}

type RawProxy struct {
	Host string `json:"host"`
}

type Proxy struct {
	Ip    string
	Port  int
	Usage int
}

type ProxyUsage struct {
	Host    string `json:"host"`
	Req     int    `json:"req"`
	Problem bool   `json:"problem"`
}

type Task struct {
	Images []Image `json:"images"`
	TaskId int     `json:"task_id"`
	Status string  `json:"status"`
	TTL    uint32  `json:"ttl"`
}

type Status struct {
	Queue int `json:"queue"`
}

func getenvStr(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return v, ErrEnvVarEmpty
	}
	return v, nil
}

func getenvBool(key string) (bool, error) {
	s, err := getenvStr(key)
	if err != nil {
		return false, err
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return false, err
	}
	return v, nil
}

func getProxy() (*Proxy, error) {

	log.WithFields(log.Fields{
		"package":    "main",
		"function":   "getProxy",
		"proxyGroup": proxyGroup,
	}).Info("Get Proxy From Api")

	resp, err := http.Get(proxyApiServer + fmt.Sprintf("/api/proxy/%s", proxyGroup))
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	//fmt.Print(resp.Body)

	var rawProxy RawProxy
	err = json.NewDecoder(resp.Body).Decode(&rawProxy)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	log.WithFields(log.Fields{
		"package":  "main",
		"function": "proxyChecker",
		"RawHost":  rawProxy.Host,
	}).Info("Check proxy")

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Fatal(err)
		}
	}(resp.Body)

	//var chunk = strings.Split(rawProxy.Host, ":")
	//var port, _ = strconv.Atoi(chunk[1])

	return &Proxy{
		Ip:    rawProxy.Host,
		Port:  9999,
		Usage: 0,
	}, nil // get the 0 index element from slice

}

func main() {

	checkerOnly, _ = getenvBool("CHECKER_ONLY")

	log.WithFields(log.Fields{
		"checkerOnly": checkerOnly,
		"package":     "main",
		"function":    "main",
	}).Info("CHECKER_ONLY")

	proxyGroup, _ = getenvStr("PROXY_GROUP")

	if proxyGroup == "" {
		log.WithFields(log.Fields{
			"package":  "main",
			"function": "main",
		}).Fatal("PROXY_GROUP not defined in env")
		os.Exit(3)
	}

	exists := false
	httpPort, exists = os.LookupEnv("PORT")
	if !exists {
		httpPort = "8080"
	}

	exists = false
	imageDir, exists = os.LookupEnv("UPLOAD_PATH")
	if !exists {
		imageDir = "./upload"
	}

	r := mux.NewRouter()

	tasks = make(map[int]Task)

	if checkerOnly == false {
		prepareProxyLoop()
	}

	r.HandleFunc("/", indexHandler).Methods("GET")
	r.HandleFunc("/task/", doCreateTask).Methods("POST")
	r.HandleFunc("/task/", doCheckTask).Methods("GET")
	r.HandleFunc("/status/", ServiceStatus).Methods("GET")

	log.WithFields(log.Fields{
		"package":    "main",
		"function":   "main",
		"server":     "http://127.0.0.1:" + httpPort,
		"proxyGroup": proxyGroup,
		"upload dir": imageDir,
	}).Info("Start Http server")

	_ = http.ListenAndServe(":"+httpPort, r)

}

func prepareProxyLoop() {

	exists := false

	proxyApiServer, exists = os.LookupEnv("PROXY_API")
	if !exists {
		log.WithFields(log.Fields{
			"package":  "main",
			"function": "main",
		}).Fatal("No Proxy Api url")
	}

	go func() {
		log.WithFields(log.Fields{
			"package":  "main",
			"function": "prepareProxyLoop",
		}).Info("Start Loop Checker")
		for {
			if !hasActiveProxy {
				syncMapMutex.Lock()

				currentProxy, err := getProxy()

				if err != nil {
					fmt.Println(err)
					return
				}

				log.WithFields(log.Fields{
					"package":  "main",
					"function": "proxyChecker",
					"check":    currentProxy.Ip,
				}).Info("Check proxy")

				proxyUrl, _ := url.Parse(fmt.Sprintf("socks5://%s", currentProxy.Ip))
				timeout := 20 * time.Second

				httpProxy := &http.Client{
					Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
					Timeout:   timeout,
				}

				checkUrl, exists := os.LookupEnv("CHECK_URL")

				if !exists {
					log.WithFields(log.Fields{
						"package":  "main",
						"function": "proxyChecker",
					}).Fatal("No check url ENV: CHECK_URL")
				}

				if _, err := httpProxy.Get(checkUrl); err == nil {
					hasActiveProxy = true
					activeProxy = currentProxy

					log.WithFields(log.Fields{
						"package":  "main",
						"function": "proxyChecker",
						"found":    activeProxy.Ip,
					}).Info("Found proxy")

				} else {
					log.WithFields(log.Fields{
						"package":  "main",
						"function": "proxyChecker",
					}).Warning("Bad proxy. Check Next")
				}

				syncMapMutex.Unlock()
			} else {
				time.Sleep(5 * time.Second)
			}
		}
	}()
}

func indexHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(403)
}

func doCreateTask(w http.ResponseWriter, r *http.Request) {

	var images Images

	err := json.NewDecoder(r.Body).Decode(&images)

	log.WithFields(log.Fields{
		"package":  "main",
		"function": "doCreateTask",
		//"body":     r.Body,
		"image": images.Images[0].Url,
	}).Info("Input Body")

	taskId := rand.Int()

	now := time.Now()

	var resp = Task{
		Images: images.Images,
		TaskId: taskId,
		Status: "inprogress",
		TTL:    uint32(now.Unix()) + taskTTL,
	}

	syncMapMutex.Lock()
	tasks[taskId] = resp
	syncMapMutex.Unlock()

	go func() {

		var newImages []Image

		for index, image := range images.Images {

			log.WithFields(log.Fields{
				"package":   "main",
				"function":  "doCreateTask",
				"Input Url": image.Url,
			}).Info("Prepare Fetch Image")

			newImageUrl := ""

			if err, newImageUrl = DoFetchImage(image); err != nil {
				log.WithFields(log.Fields{
					"package":   "main",
					"function":  "doCreateTask",
					"Input Url": image.Url,
					"error":     err,
				}).Warning("Download image Error")
				continue
			}

			log.WithFields(log.Fields{
				"package":   "main",
				"function":  "doCreateTask.go",
				"Input Url": image.Url,
				"New Url":   newImageUrl,
			}).Info("Success")

			var newImage = Image{Url: newImageUrl, Crop: false}
			newImages = append(newImages, newImage)
			images.Images[index].Url = newImageUrl
		}

		resp.Images = newImages
		resp.Status = "ready"

		syncMapMutex.Lock()
		tasks[taskId] = resp
		syncMapMutex.Unlock()

		log.WithFields(log.Fields{
			"package":     "main",
			"function":    "doCreateTask.go",
			"resp.TaskId": resp.TaskId,
			"resp.Status": resp.Status,
		}).Info("Success")

	}()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
	//w.WriteHeader(200)

	//_, _ = w.Write([]byte("Uploaded"))

}

func doCheckTask(w http.ResponseWriter, r *http.Request) {

	taskId, _ := strconv.Atoi(r.URL.Query()["taskid"][0])

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	resp, ok := tasks[taskId]

	if ok {

		_ = json.NewEncoder(w).Encode(resp)

		if resp.Status == "ready" {
			syncMapMutex.Lock()
			delete(tasks, taskId)
			syncMapMutex.Unlock()
		}

	} else {

		var errorResp = Image{
			Url: "",
		}

		_ = json.NewEncoder(w).Encode(errorResp)
		w.WriteHeader(500)

	}

}

func ServiceStatus(w http.ResponseWriter, _ *http.Request) {

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	status := Status{
		Queue: len(tasks),
	}

	_ = json.NewEncoder(w).Encode(status)

}

func DoFetchImage(image Image) (err error, out string) {

	log.WithFields(log.Fields{
		"package":   "main",
		"function":  "DoFetchImage",
		"imagePath": imageDir,
		"imageUrl":  image.Url,
	}).Info("imagePath")

	u, err := url.Parse(image.Url)
	if err != nil {
		log.Fatal(err)
	}

	log.WithFields(log.Fields{
		"package":  "main",
		"function": "DoFetchImage",
		"path":     u.Path,
	}).Info("Image URL Path")

	dir, file := filepath.Split(u.Path)

	var folderPath = imageDir + dir

	log.WithFields(log.Fields{
		"package":  "main",
		"function": "DoFetchImage",
		"dir":      dir,
		"filename": file,
		"path":     folderPath,
	}).Info("Image Param")

	u.Host = "img.gt-shop.ru"
	u.Scheme = "https"

	//Создаем директории для файла если нет
	_ = os.MkdirAll(folderPath, os.ModePerm)

	//Проверяем есть ли файл на диске

	if _, err := os.Stat(folderPath + file); err == nil {
		log.WithFields(log.Fields{
			"package":  "main",
			"function": "DoFetchImage",
			"filename": file,
			"path":     folderPath,
		}).Info("File Exist")

		return nil, u.String()
	}

	log.WithFields(log.Fields{
		"package":  "main",
		"function": "DoFetchImage",
	}).Info("Download")

	/// Если вы в режиме только проверки то скачвание не производим
	if checkerOnly == true {
		return errors.New("download Image disabled by CHECK_ONLY true env"), ""
	}

	//Скачиваем и сохраняем файл
	err = DownloadFile(folderPath+file, image.Url)

	if err != nil {
		return err, ""
	}

	//Откраываем файл для обрезки
	if image.Crop {
		if _, err := os.Stat(folderPath + file); err == nil {
			var src image2.Image
			src, err = imaging.Open(folderPath + file)

			if err != nil {
				return err, ""
			}

			//Получаем размер изображения
			imageSize := src.Bounds()
			imgWidth := imageSize.Max.X
			imgHeight := imageSize.Max.Y

			//вычитаем около 10 процентов высоты
			newImgHeight := int(float64(imgHeight) * 0.9)

			log.WithFields(log.Fields{
				"package":      "main",
				"function":     "DoFetchImage",
				"imgWidth":     imgWidth,
				"imgHeight":    imgHeight,
				"newImgHeight": newImgHeight,
			}).Info("Crop")

			dstImage128 := imaging.CropAnchor(src, imgWidth, newImgHeight, imaging.Top)
			//dstImage128 := imaging.Resize(src,200,0, imaging.Lanczos);

			imgOut, _ := os.Create(folderPath + file)
			_ = jpeg.Encode(imgOut, dstImage128, nil)
			_ = imgOut.Close()
		}
	}

	return nil, u.String()
}

func DownloadFile(filepath string, imageUrl string) (err error) {

	if !hasActiveProxy {
		return errors.New("noActiveProxy")
	}

	syncMapMutex.Lock()

	activeProxy.Usage = activeProxy.Usage + 1

	if activeProxy.Usage > proxyLimit {

		log.WithFields(log.Fields{
			"package":  "main",
			"function": "DownloadFile",
			"usage":    activeProxy.Usage,
			"limit":    proxyLimit,
			"host":     activeProxy.Ip,
		}).Info("Return Proxy and make Free")

		hasActiveProxy = false

		free := ProxyUsage{
			Host:    activeProxy.Ip,
			Req:     proxyLimit,
			Problem: false,
		}

		jsonData, err := json.Marshal(free)

		if err != nil {
			log.Fatal(err)
		}

		body, err := http.Post(
			proxyApiServer+"/api/proxy",
			"text/plain; charset=utf-8",
			bytes.NewReader(jsonData),
		)

		fmt.Printf("%s Free Proxy [%s]{%s}: %s\n",
			time.Now().Format(time.RFC3339), free.Host, jsonData, body.Status)

		if err != nil {
			fmt.Println(err)
		}

		currentProxy, err := getProxy()

		if err != nil {
			fmt.Println(err)
			return errors.New("proxyLimitReached")
		}

		activeProxy = currentProxy

		syncMapMutex.Unlock()

		log.WithFields(log.Fields{
			"package":  "main",
			"function": "DownloadFile",
			"usage":    activeProxy.Usage,
			"host":     activeProxy.Ip,
			"limit":    proxyLimit,
		}).Info("New Proxy")

		//return errors.New("proxyLimitReached")
	}
	//fmt.Printf("%s Proxy Usage %s: %d < %d\n",
	//	time.Now().Format(time.RFC3339), activeProxy.Ip,
	//	activeProxy.Usage, proxyLimit)
	syncMapMutex.Unlock()

	if activeProxy.Ip == "" {
		syncMapMutex.Lock()
		hasActiveProxy = false
		activeProxy = &Proxy{}
		syncMapMutex.Unlock()
		return errors.New("noProxyIpDefined")
	}

	// Get the data
	log.WithFields(log.Fields{
		"package":  "main",
		"function": "downloadFile",
		"imageUrl": imageUrl,
		"usage":    activeProxy.Usage,
		"limit":    proxyLimit,
		"host":     activeProxy.Ip,
	}).Info("Download Data")

	proxyUrl, err := url.Parse(fmt.Sprintf("socks5://%s", activeProxy.Ip))
	timeout := 10 * time.Second

	proxy := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
		Timeout:   timeout,
	}

	if response, err := proxy.Get(imageUrl); err != nil {
		hasActiveProxy = false
		activeProxy = &Proxy{}
		return err

	} else {

		log.WithFields(log.Fields{
			"package":     "main",
			"function":    "downloadFile",
			"status_code": response.StatusCode,
			"status":      response.Status,
		}).Info("Response")

		response.Header.Set("User-Agent", "Mozilla/5.0 (Windows; U; Windows NT 5.0; en-US; rv:1.9.2a1pre) Gecko")

		if response.StatusCode == 200 {

			log.WithFields(log.Fields{
				"package":  "main",
				"function": "downloadFile",
				"filepath": filepath,
			}).Info("Create File")

			out, err := os.Create(filepath)
			if err != nil {
				return err
			}

			log.WithFields(log.Fields{
				"package":  "main",
				"function": "downloadFile",
				"filepath": filepath,
			}).Info("Write File")

			_, err = io.Copy(out, response.Body)
			if err != nil {
				_ = os.Remove(filepath)
				return err
			}

			defer out.Close()

		} else {

			log.WithFields(log.Fields{
				"package":  "main",
				"function": "downloadFile",
				"status":   response.Status,
			}).Info("Bad Status")

			_ = os.Remove(filepath)
		}
	}

	return nil
}
