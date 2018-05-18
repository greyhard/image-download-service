package main

import (
	"net/http"
	"fmt"
	"time"
	"io"
	"strconv"
	"os"
	"html/template"
	"github.com/gorilla/mux"
	"encoding/json"
	"net/url"
	"log"
	"path/filepath"
	"flag"
	"crypto/md5"
	"github.com/disintegration/imaging"
	"image/jpeg"
	image2 "image"
)

var (
	httpPort, imageDir *string
)

func main() {

	httpPort = flag.String("port", "", "Port")
	imageDir = flag.String("dir", "", "Path")
	flag.Parse()

	r := mux.NewRouter()
	r.HandleFunc("/", indexHandler).Methods("GET")
	r.HandleFunc("/upload/", uploadHandler).Methods("GET")
	r.HandleFunc("/upload/", doUploadHandler).Methods("POST")
	r.HandleFunc("/upload/", doFetchHandler).Methods("PUT")
	fmt.Println("http://:"+*httpPort)
	fmt.Println("Upload dir: "+*imageDir)
	http.ListenAndServe(":"+*httpPort, r)
}

type Image struct {
	Url string `json:"image"`
	Crop bool `json:"crop"`
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(402)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {

	crutime := time.Now().Unix()
	h := md5.New()
	io.WriteString(h, strconv.FormatInt(crutime, 10))
	token := fmt.Sprintf("%x", h.Sum(nil))

	t, _ := template.ParseFiles("upload.gtpl")
	t.Execute(w, token)
}


func doUploadHandler(w http.ResponseWriter, r *http.Request) {

	r.ParseMultipartForm(32 << 20)
	file, handler, err := r.FormFile("uploadfile")
	if err != nil {
		w.WriteHeader(400)
		return
	}
	defer file.Close()
	//fmt.Fprintf(w, "%v", handler.Header)

	f, err := os.OpenFile("./images/"+handler.Filename, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		w.WriteHeader(503)
		return
	}
	defer f.Close()
	io.Copy(f, file)
	w.Write([]byte("Uploaded"))

}

func doFetchHandler(w http.ResponseWriter, r *http.Request){

	fmt.Println("imagePath:", *imageDir)

	var image Image
	err := json.NewDecoder(r.Body).Decode(&image)

	checkErr(err)

	u, err := url.Parse(image.Url)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(u.Path)
	dir, file := filepath.Split(u.Path)
	fmt.Println(dir)
	fmt.Println(file)

	var folderPath = *imageDir+dir
	fmt.Println(folderPath)

	u.Host = "img.gt-shop.ru"
	u.Scheme = "https"

	var resp = Image {
		Url: u.String(),
	}

	var errorResp = Image{
		Url: "",
	}

	if _, err := os.Stat(folderPath+file); err == nil {
		fmt.Println("File Exist")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err = json.NewEncoder(w).Encode(resp); err != nil {
			w.WriteHeader(500)
		}
		return
	}

	fmt.Println("Download")

	//Создаем директории для файла если нет
	os.MkdirAll(folderPath, os.ModePerm);

	//Скачиваем и сохраняем файл
	downloadFile(folderPath+file,image.Url)

	//Откраываем файл для обрезки
	if image.Crop {
		if _, err := os.Stat(folderPath + file); err == nil {
			var src image2.Image
			src, err = imaging.Open(folderPath + file)

			//Получаем размер изображения
			imageSize := src.Bounds();
			imgWidth := imageSize.Max.X
			imgHeight := imageSize.Max.Y

			//вычитаем около 10 процентов высоты
			newImgHeight := int(float64(imgHeight) * 0.9)
			fmt.Printf("%sx%s/%s\n", imgWidth, imgHeight, newImgHeight)

			if err != nil {
				log.Fatal(err)
			}

			dstImage128 := imaging.CropAnchor(src, imgWidth, newImgHeight, imaging.Top)
			//dstImage128 := imaging.Resize(src,200,0, imaging.Lanczos);

			imgOut, _ := os.Create(folderPath + file)
			jpeg.Encode(imgOut, dstImage128, nil)
			imgOut.Close()
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if _, err := os.Stat(folderPath + file); err == nil {
		if err = json.NewEncoder(w).Encode(resp); err != nil {
			w.WriteHeader(500)
		}
	} else {
		if err = json.NewEncoder(w).Encode(errorResp); err != nil {
			w.WriteHeader(500)
		}
	}
}

func checkErr(err error) {
	if err != nil {
		panic(err)
	}
}

func downloadFile(filepath string, url string) (err error) {

	// Create the file

	println(time.Now().Format(time.RFC3339), "Create File")

	out, err := os.Create(filepath)
	if err != nil  {
		return err
	}
	defer out.Close()

	// Get the data
	fmt.Printf("%s Download Data: %s\n", time.Now().Format(time.RFC3339), url)

	if response, err := http.Get(url); err != nil {
		fmt.Printf(err.Error())
	} else {

		fmt.Printf("Responce: %v   Error: %v\n", response, err)
		response.Header.Set("User-Agent", "Mozilla/5.0 (Windows; U; Windows NT 5.0; en-US; rv:1.9.2a1pre) Gecko")

		t_time := time.Now()

		if err != nil {
			fmt.Println("Hello")
		} else {
			if response.StatusCode == 200 {
				fmt.Println(t_time.Format(time.RFC3339), "OK")
				println("Write File")
				_, err = io.Copy(out, response.Body)
				if err != nil {
					os.Remove(filepath)
					return err
				}
			} else {
				fmt.Println(t_time.Format(time.RFC3339), "BAD")
				os.Remove(filepath)
				return fmt.Errorf("bad status: %s", response.Status)
			}
		}

	}

	//resp, err := http.Get(url)
	//if err != nil {
	//	os.Remove(filepath)
	//	return err
	//}
	//defer response.Body.Close()

	//// Check server response
	//println("Check status")
	//if response.StatusCode != http.StatusOK {
	//	os.Remove(filepath)
	//	return fmt.Errorf("bad status: %s", resp.Status)
	//}

	// Writer the body to file

	return nil
}