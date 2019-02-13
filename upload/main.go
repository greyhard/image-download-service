package main

import (
    "bufio"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "github.com/disintegration/imaging"
    "github.com/gorilla/mux"
    image2 "image"
    "image/jpeg"
    "io"
    "log"
    "math/rand"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strconv"
    "sync"
    "time"
)

var (
    httpPort, imageDir *string
    tasks              map[int]Task
    proxy              []Proxy
    syncMapMutex       = sync.RWMutex{}
    hasActiveProxy     = false
    activeProxy        Proxy
    proxyLimit         = 20
)

func main() {

    tasks = make(map[int]Task)
    //proxy = make(map[string]Proxy)

    if err := loadProxy(); err != nil {
        log.Fatal(err)
    }

    go func() {
        for {
            time.Sleep(60 * time.Second)
            fmt.Printf("%s Cleanup: \n", time.Now().Format(time.RFC3339))
        }
    }()

    go func() {
        for {

            if !hasActiveProxy {

                if len(proxy) > 0 {
                    currentProxy := proxy[0] // get the 0 index element from slice
                    proxy = proxy[1:]        // remove the 0 index element from slice
                    hasActiveProxy = true
                    activeProxy = currentProxy
                    fmt.Printf("%s Use proxy: %s Left %d \n", time.Now().Format(time.RFC3339), activeProxy.Ip, len(proxy))
                } else {
                    if err := loadProxy(); err != nil {
                        log.Fatal(err)
                    }
                }
            }

            fmt.Printf("%s Proxy checker\n", time.Now().Format(time.RFC3339))
            time.Sleep(5 * time.Second)

        }

    }()

    httpPort = flag.String("port", "", "Port")
    imageDir = flag.String("dir", "", "Path")
    flag.Parse()

    r := mux.NewRouter()
    r.HandleFunc("/", indexHandler).Methods("GET")
    r.HandleFunc("/task/", doCreateTask).Methods("POST")
    r.HandleFunc("/task/", doCheckTask).Methods("GET")
    r.HandleFunc("/status/", doStatus).Methods("GET")

    fmt.Printf("%s http://:%s\n", time.Now().Format(time.RFC3339), *httpPort)
    fmt.Printf("%s Upload dir: %s\n", time.Now().Format(time.RFC3339), *imageDir)
    _ = http.ListenAndServe(":"+*httpPort, r)

}

type Images struct {
    Images []Image `json:"images"`
}

type Image struct {
    Url  string `json:"image"`
    Crop bool   `json:"crop"`
}

type Proxy struct {
    Ip    string
    Port  int
    Usage int
}

type Task struct {
    Images []Image `json:"images"`
    TaskId int     `json:"task_id"`
    Status string  `json:"status"`
}

type Status struct {
    Queue int `json:"queue"`
}

func indexHandler(w http.ResponseWriter, r *http.Request) {

    w.WriteHeader(402)
}

func doCreateTask(w http.ResponseWriter, r *http.Request) {

    fmt.Printf("%s Queue: %d\n", time.Now().Format(time.RFC3339), len(tasks))

    var images Images
    err := json.NewDecoder(r.Body).Decode(&images)

    if err != nil {
        w.WriteHeader(500)
        return
    }

    taskId := rand.Int()

    var resp = Task{
        Images: images.Images,
        TaskId: taskId,
        Status: "inprogress",
    }

    syncMapMutex.Lock()
    tasks[taskId] = resp
    syncMapMutex.Unlock()

    go func() {

        var newImages []Image

        for index, image := range images.Images {

            fmt.Printf("%s Input Url: %s\n", time.Now().Format(time.RFC3339), image.Url)

            err, newImageUrl := doFetchImage(image)

            if err != nil {
                fmt.Printf("%s Downloaderror2\n", time.Now().Format(time.RFC3339))
                log.Println(err)
            }
            if err == nil {

                fmt.Printf("%s New Url: %s\n", time.Now().Format(time.RFC3339), newImageUrl)

                var newImage = Image{Url: newImageUrl, Crop: false}
                newImages = append(newImages, newImage)
                images.Images[index].Url = newImageUrl
            }
        }

        resp.Images = newImages
        resp.Status = "ready"
        tasks[resp.TaskId] = resp

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


func doStatus(w http.ResponseWriter, r *http.Request) {

    w.Header().Set("Content-Type", "application/json; charset=utf-8")

    status := Status{
        Queue: len(tasks),
    }

    _ = json.NewEncoder(w).Encode(status)

}

func doFetchImage(image Image) (err error, out string){

    fmt.Printf("%s imagePath: %s\n", time.Now().Format(time.RFC3339), *imageDir)

    u, err := url.Parse(image.Url)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("%s Image URL Path: %s\n", time.Now().Format(time.RFC3339), u.Path)
    dir, file := filepath.Split(u.Path)
    fmt.Printf("%s Image DST Dir: %s\n", time.Now().Format(time.RFC3339), dir)
    fmt.Printf("%s Image DST Filename: %s\n", time.Now().Format(time.RFC3339), file)

    var folderPath = *imageDir + dir

    fmt.Printf("%s Image Full dir path: %s\n", time.Now().Format(time.RFC3339), folderPath)

    u.Host = "img.gt-shop.ru"
    u.Scheme = "https"

    //Создаем директории для файла если нет
    _ = os.MkdirAll(folderPath, os.ModePerm)

    //Проверяем есть ли файл на диске

    if _, err := os.Stat(folderPath + file); err == nil {
        println(time.Now().Format(time.RFC3339), "File Exist:", folderPath+file)
        return nil, u.String()
    }

    println(time.Now().Format(time.RFC3339), "Download")

    //Скачиваем и сохраняем файл
    err = downloadFile(folderPath+file, image.Url)

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

            fmt.Printf("%s Crop: %dx%d/%d \n", time.Now().Format(time.RFC3339), imgWidth, imgHeight, newImgHeight)

            if err != nil {
                log.Fatal(err)
            }

            dstImage128 := imaging.CropAnchor(src, imgWidth, newImgHeight, imaging.Top)
            //dstImage128 := imaging.Resize(src,200,0, imaging.Lanczos);

            imgOut, _ := os.Create(folderPath + file)
            _ = jpeg.Encode(imgOut, dstImage128, nil)
            _ = imgOut.Close()
        }
    }

    return nil, u.String()
}

func downloadFile(filepath string, imageUrl string) (err error) {

    if !hasActiveProxy {
        return errors.New("noActiveProxy")
    }

    activeProxy.Usage = activeProxy.Usage + 1

    if activeProxy.Usage > proxyLimit {
        hasActiveProxy = false
        activeProxy = Proxy{}
        return errors.New("proxyLimitReached")
    } else {
        fmt.Printf("%s Proxy Usage %s: %d < %d\n",
            time.Now().Format(time.RFC3339), activeProxy.Ip,
            activeProxy.Usage, proxyLimit)
    }

    if activeProxy.Ip == "" {
        hasActiveProxy = false
        activeProxy = Proxy{}
        return errors.New("noProxyIpDefined")
    }

    // Get the data
    fmt.Printf("%s Download Data: %s\n", time.Now().Format(time.RFC3339), imageUrl)

    proxyUrl, err := url.Parse(fmt.Sprintf("socks5://%s", activeProxy.Ip))
    timeout := time.Duration(10 * time.Second)

    proxy := &http.Client{
        Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
        Timeout:   timeout,
    }

    if response, err := proxy.Get(imageUrl); err != nil {
        hasActiveProxy = false
        activeProxy = Proxy{}
        fmt.Printf("%s Download Error\n", time.Now().Format(time.RFC3339))
        return err

    } else {

        fmt.Printf("%s Responce: %v   Error: %v\n", time.Now().Format(time.RFC3339), response, err)
        response.Header.Set("User-Agent", "Mozilla/5.0 (Windows; U; Windows NT 5.0; en-US; rv:1.9.2a1pre) Gecko")

        tTime := time.Now()

        if response.StatusCode == 200 {
            fmt.Println(tTime.Format(time.RFC3339), "OK")

            println(time.Now().Format(time.RFC3339), "Create File:", filepath)

            out, err := os.Create(filepath)
            if err != nil {
                return err
            }

            fmt.Printf("%s Write File: %s\n", time.Now().Format(time.RFC3339), filepath)

            _, err = io.Copy(out, response.Body)
            if err != nil {
                _ = os.Remove(filepath)
                return err
            }

            defer out.Close()

        } else {
            fmt.Println(tTime.Format(time.RFC3339), "BAD")
            _ = os.Remove(filepath)
            return fmt.Errorf("bad status: %s", response.Status)
        }

    }

    return nil
}

func loadProxy() (err error) {

    if file, err := os.Open("proxy.txt"); err == nil {

        scanner := bufio.NewScanner(file)
        for scanner.Scan() {
            fmt.Println(scanner.Text())

            proxy = append(proxy, Proxy{
                Ip:    scanner.Text(),
                Port:  9999,
                Usage: 0,
            })

        }

        proxy = Shuffle(proxy)

        return nil

    }

    return errors.New("cantOpenProxyFile")
}

func Shuffle(vals []Proxy) []Proxy {
    r := rand.New(rand.NewSource(time.Now().Unix()))
    ret := make([]Proxy, len(vals))
    n := len(vals)
    for i := 0; i < n; i++ {
        randIndex := r.Intn(len(vals))
        ret[i] = vals[randIndex]
        vals = append(vals[:randIndex], vals[randIndex+1:]...)
    }
    return ret
}
