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

var tesseract_bin string = "tesseract"
var convert_bin string = "convert"

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

func convertImage(filename string, job string) (string, bool) {

	hasError := false
	new_filename := filename + "-" + job

	var cmd *exec.Cmd

	if job == "all" {
		cmd = exec.Command(convert_bin, filename,
			// Cut the top and bottom off
			"-crop", "+0+200", "-crop", "+0+-60", "+repage",
			"-channel", "rgba", "-alpha", "set", "-fuzz", "15%", "-fill", "white", "-opaque", "#999999",
			new_filename)
	} else if job == "in" {
		cmd = exec.Command(convert_bin, filename,
			// Focus on incoming text
			"-crop", "+0+200", "-crop", "+0+-60", "+repage",
			"-channel", "rgba", "-alpha", "set", "-fuzz", "40%", "-fill", "white", "-opaque", "#1D62F0", "-opaque", "#0BD318",
			new_filename)
	} else if job == "out" {
		cmd = exec.Command(convert_bin, filename,
			// Focus on outgoing text
			"-crop", "+0+200", "-crop", "+0+-60", "+repage",
			"-channel", "rgba", "-alpha", "set", "-fuzz", "20%", "-fill", "black", "-opaque", "#DBDDDE", "-opaque", "#000000", "-opaque", "#ffffff", "-opaque", "#cbcbcb",
			new_filename)
	} else {
		return "", true
	}

	err := cmd.Start()
	if err != nil {
		panic(err)
		hasError = true
	}

	err = cmd.Wait()
	if err != nil {
		panic(err)
		hasError = true
	}

	return new_filename, hasError
}

func tesseract(filename string, lineDelimiter string) []string {
	outfile := filename + ".tesseract"

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

	return regexp.MustCompile(lineDelimiter).Split(string(dat), -1)
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
	Lines    []map[string]string
}

func ErrorResp(w http.ResponseWriter, msg string) {
	resp := new(ScanResponse)
	resp.Error = true
	resp.ErrorMsg = msg
	fmt.Fprintf(w, respond(resp))
}

func ScanHandler(w http.ResponseWriter, r *http.Request) {
	file, header, err := r.FormFile("file")

	if err != nil {
		ErrorResp(w, "Missing file upload.")
		return
	}

	defer file.Close()

	tmpFilename := "/tmp/" + md5hash("original-"+strconv.FormatInt(time.Now().Unix(), 10))

	out, err := os.Create(tmpFilename)
	if err != nil {
		ErrorResp(w, "Can't create temp file.")
		return
	}

	defer out.Close()

	_, err = io.Copy(out, file)
	if err != nil {
		ErrorResp(w, "Can't copy to temp file.")
		return
	}

	if isMessages(tmpFilename) == false {
		ErrorResp(w, "Not a text message screenshot.")
		return
	}

	// Prep and OCR full image
	trimmedFile, hasError := convertImage(tmpFilename, "all")
	if hasError {
		ErrorResp(w, "Failed to trim image.")
		return
	}

	allLines := tesseract(trimmedFile, "\n")

	// Prep and OCR incoming
	incomingFile, hasError := convertImage(tmpFilename, "in")
	if hasError {
		ErrorResp(w, "Image optimization failed (in).")
		return
	}

	inLines := tesseract(incomingFile, "\n")

	// Prep and OCR outgoing
	outgoingFile, hasError := convertImage(tmpFilename, "out")
	if hasError {
		ErrorResp(w, "Image optimization failed (out).")
		return
	}

	outLines := tesseract(outgoingFile, "\n")

	returnLines := make([]map[string]string, len(allLines))

	for i := range allLines {
		if strings.TrimSpace(allLines[i]) == "" {
			continue
		}

		lineType := "unknown"

		for k := range inLines {
			if strings.TrimSpace(inLines[k]) == strings.TrimSpace(allLines[i]) {
				lineType = "incoming"
				break
			}
		}

		if lineType == "unknown" {
			for k := range outLines {
				if strings.TrimSpace(outLines[k]) == strings.TrimSpace(allLines[i]) {
					lineType = "outgoing"
					break
				}
			}
		}

		returnLines[i] = map[string]string{
			"type": lineType,
			"text": allLines[i],
		}
	}

	resp := new(ScanResponse)
	resp.Error = false
	resp.TmpFile = header.Filename
	resp.Lines = returnLines

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, respond(resp))
}

func main() {
	// Handle command line arguments
	staticPath := "./"
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
