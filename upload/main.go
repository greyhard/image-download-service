package main

import (
    "encoding/json"
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
    "time"
)

var (
    httpPort, imageDir *string
    tasks              map[int]Task
)

func main() {

    tasks = make(map[int]Task)

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

    tasks[taskId] = resp

    go func() {

        var newImages []Image

        for index, image := range images.Images {

            fmt.Printf("%s Input Url: %s\n", time.Now().Format(time.RFC3339), image.Url)

            err, newImageUrl := doFetchImage(image)

            if err != nil {
                fmt.Printf("%s Downloaderror2\n", time.Now().Format(time.RFC3339))
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
            delete(tasks, taskId)
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

func downloadFile(filepath string, url string) (err error) {

    // Get the data
    fmt.Printf("%s Download Data: %s\n", time.Now().Format(time.RFC3339), url)

    if response, err := http.Get(url); err != nil {
        fmt.Printf("%s Download Error\n", time.Now().Format(time.RFC3339))

        return err
    } else {

        fmt.Printf("%s Responce: %v   Error: %v\n", time.Now().Format(time.RFC3339), response, err)
        response.Header.Set("User-Agent", "Mozilla/5.0 (Windows; U; Windows NT 5.0; en-US; rv:1.9.2a1pre) Gecko")

        tTime := time.Now()

        if err != nil {
            fmt.Println("Hello")
        } else {
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

    }

    return nil
}