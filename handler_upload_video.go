package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	const maxMemory = 1 << 30
	videoIDStr := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDStr)

	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error parsing videoid", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "couldn't extract token", err)
		return
	}

	userID, err := auth.ValidateJWT(token, os.Getenv("JWT_SECRET"))
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "couldn't validate jwt", err)
		return
	}

	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusNotFound, "coultn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusForbidden, "youre not allowed to upload the video", nil)
		return
	}

	err = r.ParseMultipartForm(maxMemory)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "size exceeded", err)
		return
	}

	multiPartFile, multiPartHeader, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "couldn't get multipartfile", err)
		return
	}
	defer multiPartFile.Close()

	mpFileType, _, err := mime.ParseMediaType(multiPartHeader.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "error getting file type", err)
		return
	}
	if mpFileType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "invalid format", nil)
		return
	}

	tempFile, err := os.CreateTemp("", "tubely-*.mp4")
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error creating temp file", err)
		return
	}
	defer tempFile.Close()
	defer os.Remove(tempFile.Name())
	_, err = io.Copy(tempFile, multiPartFile)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error copying file", err)
		return
	}

	_, err = tempFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error changing pointer to file", err)
		return
	}

	newFileStr, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error processing video for fast start", err)
		return
	}
	newFile, err := os.Open(newFileStr)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error opening file processed", err)
		return
	}
	defer newFile.Close()
	defer os.Remove(newFileStr)

	_, err = newFile.Seek(0, io.SeekStart)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error changing pointer to file", err)
		return
	}

	randName := make([]byte, 32)
	_, err = rand.Read(randName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "couldn't generate name", err)
		return
	}
	key := hex.EncodeToString(randName) + ".mp4"
	mimeMp4 := "video/mp4"
	aspect, err := getVideoAspectRatio(newFileStr)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldnt get aspect ratio", err)
		return
	}
	s3Key := aspect + "/" + key
	var params s3.PutObjectInput
	params.Bucket = &cfg.s3Bucket
	params.Key = &s3Key
	params.Body = newFile
	params.ContentType = &mimeMp4

	_, err = cfg.s3client.PutObject(r.Context(), &params)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldnt put object", err)
		return
	}

	videoURL := cfg.s3CfDistribution + "/" + s3Key

	video.VideoURL = &videoURL

	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "error updating video db", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}

func getVideoAspectRatio(filePath string) (string, error) {
	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var resultFromCmd bytes.Buffer
	cmd.Stdout = &resultFromCmd
	err := cmd.Run()

	if err != nil {
		return "", err
	}

	type widthHeightResp struct {
		Streams []struct {
			Width  int `json:"width,omitempty"`
			Height int `json:"height,omitempty"`
		} `json:"streams"`
	}

	var res widthHeightResp
	err = json.Unmarshal(resultFromCmd.Bytes(), &res)

	if err != nil {
		return "", err
	}

	return irreductibles(res.Streams[0].Width, res.Streams[0].Height), nil
}

func irreductibles(a, b int) string {
	const tolerance = 0.1
	const ratio = 16.0 / 9.0
	c := float64(a) / float64(b)
	if c > ratio-tolerance && c < ratio+tolerance {
		return "landscape"
	} else if c > 1/ratio-tolerance && c < 1/ratio+tolerance {
		return "portrait"
	} else {
		return "other"
	}
}

func processVideoForFastStart(filePath string) (string, error) {
	fmt.Printf("filepath: %s ext:%s\n", filePath, filepath.Ext(filePath))
	if filePath == "" || !strings.EqualFold(filepath.Ext(filePath), ".mp4") {
		return "", fmt.Errorf("invalid input")
	}

	if _, err := os.Stat(filePath); err != nil {
		return "", err
	}

	tmp := fmt.Sprintf("%s.tmp", filePath)
	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", tmp)
	cmd.Stderr = &stderr
	err := cmd.Run()

	if err != nil {
		return "", fmt.Errorf("ffmpeg error : %v: %s", err, stderr.String())
	}

	return tmp, nil
}

/*func generatePresignedURL(s3client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	s3presignedclient := s3.NewPresignClient(s3client)
	var getObjInput s3.GetObjectInput
	getObjInput.Bucket = &bucket
	getObjInput.Key = &key
	presignedReq, err := s3presignedclient.PresignGetObject(context.Background(), &getObjInput, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", err
	}

	return presignedReq.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	fmt.Printf("url: %s\n", *video.VideoURL)
	urlArr := strings.Split(*video.VideoURL, ",")
	if len(urlArr) != 2 {
		return database.Video{}, fmt.Errorf("incorrect url ")
	}

	fmt.Printf("%s:%s\n\n", urlArr[0], urlArr[1])

	url, err := generatePresignedURL(cfg.s3client, urlArr[0], urlArr[1], time.Hour)
	if err != nil {
		return database.Video{}, err
	}
	video.VideoURL = &url
	return video, nil
}*/
