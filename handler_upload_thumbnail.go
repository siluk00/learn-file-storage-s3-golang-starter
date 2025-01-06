package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	const maxMemory = 10 << 20
	err = r.ParseMultipartForm(maxMemory)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Couldn't Multipart form", err)
		return
	}

	formFile, formFileHeader, err := r.FormFile("thumbnail")

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldnt read formfile", err)
		return
	}

	defer formFile.Close()
	contentType := formFileHeader.Header.Get("Content-Type")

	video, err := cfg.db.GetVideo(videoID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't find video on db", err)
		return
	}

	mediaType, _, err := mime.ParseMediaType(contentType)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing media type", err)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "wrong media type", nil)
		return
	}

	randName := make([]byte, 32)
	_, err = rand.Read(randName)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "unable to generate id", err)
	}

	// First, create just the base filename (without assets/)
	baseFileName := base64.RawURLEncoding.EncodeToString(randName) + "." + strings.Split(mediaType, "/")[1]

	// Create the full URL for the database (with assets/)
	thumbnailUrl := "assets/" + baseFileName

	// Create the full filesystem path
	url := filepath.Join(cfg.assetsRoot, baseFileName)

	// Create the file (no need for TrimPrefix)
	fileThumb, err := os.Create(url)

	fmt.Println("Database URL:", thumbnailUrl)
	fmt.Println("File save path:", url)
	fmt.Println("Assets root:", cfg.assetsRoot)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating file", err)
		return
	}

	_, err = io.Copy(fileThumb, formFile)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying files", err)
		return
	}

	video.ThumbnailURL = &thumbnailUrl
	err = cfg.db.UpdateVideo(video)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update video", err)
		return
	}
	fileInfo, _ := os.Stat(url)
	if fileInfo != nil {
		fmt.Println("File size:", fileInfo.Size())
		fmt.Println("File permissions:", fileInfo.Mode())
	}

	respondWithJSON(w, http.StatusOK, &video)
}
