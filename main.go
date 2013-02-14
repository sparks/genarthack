package main

import (
	"archive/zip"
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	resourcePath = "templates/resources/"
	templatePath = "templates/"
	uploadPath   = "uploads/"
	cookieName   = "gahpsess"
)

var templates = template.Must(template.ParseFiles(filepath.Join(templatePath, "status.html")))
var titleValidator = regexp.MustCompile("^[a-zA-Z0-9_. ]+$")

type StatusPage struct {
	Message  string
	Redirect string
	Timeout  int
}

var usercount int = 0
var secret string = "DEFAULT"
var secretVal string

func main() {
	err := os.Mkdir(uploadPath, os.ModeDir|0755)
	if err != nil && !os.IsExist(err) {
		panic(err)
	}

	// Read the secret
	file, err := os.Open("secret.txt")
	if err == nil {
		reader := bufio.NewReader(file)
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal("Failed to read secret file")
		}
		secret = strings.TrimSpace(line)
		log.Println("Set the secret to: " + secret)
	} else {
		log.Println("Warning: No secret specified, defaulting to: " + secret)
	}

	// Create a random value for the cookies
	bytes := make([]byte, 4)
	n, err := rand.Read(bytes)
	if err != nil || n != cap(bytes) {
		log.Fatal("Failed to initalize random session value")
	}
	secretVal = hex.EncodeToString(bytes)

	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {}) //Prevent favicon unserved error

	http.HandleFunc("/auth", AuthHandler)
	http.HandleFunc("/submit", SubmitHandler)

	http.Handle("/uploads/", http.StripPrefix("/uploads", http.FileServer(http.Dir(uploadPath))))
	http.Handle("/resources/", http.StripPrefix("/resources", http.FileServer(http.Dir(resourcePath))))

	http.HandleFunc("/random", RandomHandler)
	http.HandleFunc("/cycler", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(templatePath, "cycler.html"))
	})

	http.ListenAndServe(":8080", nil)
}

func AuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		log.Printf("Password: %s\n", r.FormValue("pass"))
		if r.FormValue("pass") == secret {
			var cookie http.Cookie
			cookie.Name = cookieName
			cookie.Value = secretVal
			http.SetCookie(w, &cookie)
			ServeStatus(w, &StatusPage{"Login Sucessful", "/submit", 1})
			return
		}
	}

	http.ServeFile(w, r, templatePath+"auth.html")
}

func RandomHandler(w http.ResponseWriter, r *http.Request) {
	listing, err := ioutil.ReadDir(uploadPath)

	if err != nil || len(listing) == 0 {
		ServeStatus(w, &StatusPage{"No content", "/submit", 2})
		return
	}

	dirs := make([]os.FileInfo, 0, len(listing))

	for _, fileInfo := range listing {
		if fileInfo.IsDir() {
			dirs = dirs[0 : len(dirs)+1]
			dirs[len(dirs)-1] = fileInfo
		}
	}

	if len(dirs) == 0 {
		ServeStatus(w, &StatusPage{"No content", "/submit", 2})
		return
	}

	http.Redirect(w, r, "/"+uploadPath+dirs[usercount].Name()+"/live/", http.StatusFound)

	usercount++
	if usercount >= len(dirs) {
		usercount = 0
	}
}

func SubmitHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(cookieName)

	if err != nil || cookie.Value != secretVal {
		http.Redirect(w, r, "/auth", http.StatusTemporaryRedirect)
		return
	}

	if r.Method == "GET" {
		http.ServeFile(w, r, templatePath+"submit.html")
	} else if r.Method == "POST" {
		t := time.Now()
		timestamp := t.Format("2006-01-02 15:04:05.000")

		title := r.FormValue("title")

		if !titleValidator.MatchString(title) {
			ServeStatus(w, &StatusPage{"Invalid Title", "", -1})
			return
		}

		uploadFile, header, err := r.FormFile("file")
		if err != nil {
			ServeStatus(w, &StatusPage{"Invalid or Missing File", "", -1})
			return
		}

		if !strings.HasSuffix(header.Filename, ".zip") {
			ServeStatus(w, &StatusPage{"Non Zip File", "", -1})
			return
		}

		pieceDir := filepath.Join(uploadPath, title)

		err = os.Mkdir(pieceDir, os.ModeDir|0755)
		if os.IsExist(err) {
			ServeStatus(w, &StatusPage{"That title is taken", "", -1})
			return
		} else if err != nil {
			ServeStatus(w, &StatusPage{"Error Making Piece Directories", "", -1})
			return
		}

		pieceArchiveDir := filepath.Join(pieceDir, "archive")

		err = os.MkdirAll(pieceArchiveDir, os.ModeDir|0755)

		if err != nil {
			ServeStatus(w, &StatusPage{"Error Making Piece Directories", "", -1})
			return
		}

		zipPath := filepath.Join(pieceArchiveDir, timestamp+".zip")

		osFile, err := os.Create(zipPath)
		if err != nil {
			ServeStatus(w, &StatusPage{"Error Creating File", "", -1})
			log.Println(err)
			return
		}

		io.Copy(osFile, uploadFile)
		osFile.Close()

		// Open a zip archive for reading
		zipReader, err := zip.OpenReader(zipPath)
		if err != nil {
			ServeStatus(w, &StatusPage{"Malformed Zip", "", -1})
			return
		}
		defer zipReader.Close()

		tmpUnzipDir := filepath.Join(pieceDir, "tmp")

		err = os.Mkdir(tmpUnzipDir, os.ModeDir|0755)
		if err != nil && !os.IsExist(err) {
			ServeStatus(w, &StatusPage{"Error Making Unzip Directory", "", -1})
			return
		}

		fileErr := false
		zipErr := false

		for _, zipFile := range zipReader.File {
			if strings.HasSuffix(zipFile.Name, "/") {
				os.MkdirAll(filepath.Join(tmpUnzipDir, zipFile.Name), os.ModeDir|0755)
			} else {
				newFile, err := os.Create(filepath.Join(tmpUnzipDir, zipFile.Name))
				if err != nil {
					panic(err)
					fileErr = true
				}

				zipFileReader, err := zipFile.Open()
				if err != nil {
					panic(err)
					zipErr = true
				}

				_, err = io.Copy(newFile, zipFileReader)
				if err != nil {
					panic(err)
					fileErr = true
				}

				zipFileReader.Close()
				newFile.Close()
			}
		}

		if zipErr || fileErr {
			p := &StatusPage{"Errors Unzipping:", "", -1}

			if zipErr {
				p.Message += " zip malformed"
			}

			if fileErr {
				p.Message += " couldn't create file"
			}

			os.RemoveAll(tmpUnzipDir)

			ServeStatus(w, p)
			return
		}

		pieceLiveDir := filepath.Join(pieceDir, "live")
		os.RemoveAll(pieceLiveDir)

		rootTmpDir, err := findRoot(tmpUnzipDir)
		if err != nil {
			log.Print("Problem finding root: ")
			log.Println(err)
		}

		log.Println(rootTmpDir)
		err = os.Rename(rootTmpDir, pieceLiveDir)

		os.RemoveAll(tmpUnzipDir)

		if err != nil {
			log.Println(err)
			ServeStatus(w, &StatusPage{"Problem Staging Project", "", -1})
			return
		}

		ServeStatus(w, &StatusPage{"Sucessful Upload", pieceLiveDir, 2})
	}
}

func findRoot(rootpath string) (string, error) {
	files, err := ioutil.ReadDir(rootpath)
	if err != nil {
		log.Println("Problem reading directory during find root")
		return rootpath, err
	}

	for _, finfo := range files {
		if !finfo.IsDir() && strings.Contains(finfo.Name(), "index.htm") {
			return rootpath, nil
		}
	}

	for _, finfo := range files {
		if finfo.IsDir() {
			subpath, err := findRoot(filepath.Join(rootpath, finfo.Name()))
			if err == nil {
				return subpath, nil
			}
		}
	}

	return rootpath, errors.New("No index.html found anywhere in subdirectories")
}

func ServeStatus(w http.ResponseWriter, s *StatusPage) {
	err := templates.ExecuteTemplate(w, "status.html", s)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
