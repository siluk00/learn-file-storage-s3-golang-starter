package main

import (
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
	/*file, err := io.ReadAll(formFile)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error reading file", err)
		return
	}*/

	video, err := cfg.db.GetVideo(videoID)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't find video on db", err)
		return
	}

	//fileStr := base64.StdEncoding.EncodeToString(file)
	//dataUrl := "data:" + contentType + ";base64," + fileStr

	/*
		var thumb thumbnail
		thumb.mediaType = contentType
		thumb.data = file
		videoThumbnails[videoID] = thumb
	*/

	mediaType, _, err := mime.ParseMediaType(contentType)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error parsing media type", err)
		return
	}

	if mediaType != "image/jpeg" && mediaType != "image/png" {
		respondWithError(w, http.StatusBadRequest, "wrong media type", err)
		return
	}

	extension := strings.Split(contentType, "/")[1]
	url := filepath.Join(cfg.assetsRoot, videoIDString+"."+extension)
	fmt.Println(url)
	fileThumb, err := os.Create(url)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating file", err)
		return
	}

	_, err = io.Copy(fileThumb, formFile)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying files", err)
		return
	}

	thumbnailUrl := "http://localhost:" + cfg.port + "/" + url
	fmt.Println(thumbnailUrl)

	video.ThumbnailURL = &thumbnailUrl
	err = cfg.db.UpdateVideo(video)

	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, &video)
}
