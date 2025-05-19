package main

import (
	"crypto/rand"
	"fmt"
	"mime"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	const maxMemory = 100 << 20 // 100 MB
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType := header.Header.Get("Content-Type")
	if mediaType == "" {
		respondWithError(w, http.StatusBadRequest, "Missing Content-Type for video", nil)
		return
	}
	mimeMediaType, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type for video", err)
		return
	}
	if !strings.HasPrefix(mimeMediaType, "video/") {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type for video", nil)
		return
	}

	// Generate a random 32-byte hex string for the file key
	randBytes := make([]byte, 32)
	if _, err := rand.Read(randBytes); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate random ID", err)
		return
	}
	fileKey := fmt.Sprintf("%x%s", randBytes, mediaTypeToExt(mimeMediaType))

	// Upload to S3
	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket:      &cfg.s3Bucket,
		Key:         &fileKey,
		Body:        file,
		ContentType: &mimeMediaType,
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't upload to S3", err)
		return
	}

	// Get the video record
	video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to update this video", nil)
		return
	}

	// Update the video URL in the database
	videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileKey)
	video.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	respondWithJSON(w, http.StatusOK, video)
}
