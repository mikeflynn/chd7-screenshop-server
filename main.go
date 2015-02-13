package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

var tesseract_bin string = "/usr/local/bin/tesseract"

func respond(resp interface{}) string {
	jsonStr, _ := json.Marshal(resp)
	return string(jsonStr)
}

func md5hash(input string) string {
	hash := md5.Sum([]byte(input))
	return hex.EncodeToString(hash[:])
}

func isMessages(filename string) bool {
	outfile := "/tmp/" + md5hash(filename) + "-full.tesseract"

	cmd := exec.Command(tesseract_bin, filename, outfile)
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	cmd.Wait()

	dat, err := ioutil.ReadFile(outfile + ".txt")
	if err != nil {
		panic(err)
		return false
	}

	//fmt.Println(string(dat))

	if strings.HasSuffix(strings.TrimSpace(string(dat)), "Send") {
		return true
	}

	return false
}

func trimImage(filename string) bool {
	cmd := exec.Command("/usr/local/bin/convert", filename, "-crop", "+0+200", "-crop", "-200+-50", "+repage", filename)
	err := cmd.Start()
	if err != nil {
		panic(err)
		return false
	}

	err = cmd.Wait()
	if err != nil {
		panic(err)
		return false
	}

	return true
}

func tesseract(filename string) []string {
	outfile := "/tmp/" + md5hash(filename) + ".tesseract"

	cmd := exec.Command(tesseract_bin, filename, outfile)
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}

	cmd.Wait()

	dat, err := ioutil.ReadFile(outfile + ".txt")
	if err != nil {
		panic(err)
		return nil
	}

	return regexp.MustCompile("\n\n").Split(string(dat), -1)
}

// Handlers

func IndexHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, respond(map[string]string{"foo": "bar"}))
}

type ScanResponse struct {
	Error    bool
	ErrorMsg string
	TmpFile  string
	Lines    []string
}

func ScanHandler(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")

	if err != nil {
		resp := new(ScanResponse)
		resp.Error = true
		resp.ErrorMsg = "Missing file upload."
		fmt.Fprintf(w, respond(resp))
		return
	}

	defer file.Close()

	tmpFilename := "/tmp/" + md5hash("original-"+strconv.FormatInt(time.Now().Unix(), 10))

	out, err := os.Create(tmpFilename)
	if err != nil {
		resp := new(ScanResponse)
		resp.Error = true
		resp.ErrorMsg = "Can't create temp file."
		fmt.Fprintf(w, respond(resp))
		return
	}

	defer out.Close()

	_, err = io.Copy(out, file)
	if err != nil {
		resp := new(ScanResponse)
		resp.Error = true
		resp.ErrorMsg = "Can't copy to temp file."
		fmt.Fprintf(w, respond(resp))
		return
	}

	if isMessages(tmpFilename) == false {
		resp := new(ScanResponse)
		resp.Error = true
		resp.ErrorMsg = "Not a text message screenshot."
		fmt.Fprintf(w, respond(resp))
		return
	}

	trimImage(tmpFilename)

	lines := tesseract(tmpFilename)

	resp := new(ScanResponse)
	resp.Error = false
	resp.TmpFile = header.Filename
	resp.Lines = lines

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, respond(resp))
}

func main() {
	// Handle command line arguments
	staticPath := "."
	if len(os.Args[1:]) != 0 {
		staticPath = os.Args[1]
	}

	// Start web server
	router := mux.NewRouter().StrictSlash(true)
	router.HandleFunc("/", IndexHandler)
	router.HandleFunc("/scan", ScanHandler).Methods("POST")
	http.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path[1:]
		if strings.HasSuffix(path, "/") {
			path = path + "index.html"
		}

		http.ServeFile(w, r, staticPath+path)
	})
	http.Handle("/", router)

	log.Fatal(http.ListenAndServe(":8085", nil))
}
