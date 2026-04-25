package runtimeapi

import (
	"net/http"
	"strings"

	"github.com/Alice-space/alice/internal/logging"
	"github.com/gin-gonic/gin"
)

func (s *Server) handleSendImage(c *gin.Context) {
	if !s.allowRuntimeMessage() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime message is disabled for this bot"})
		return
	}
	if s.sender == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sender is unavailable"})
		return
	}
	session, err := sessionContextFromHeaders(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req ImageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	imageKey := strings.TrimSpace(req.ImageKey)
	if imageKey == "" {
		if err := validatePathUnderRoot(req.Path, session.ResourceRoot); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uploaded, err := s.sender.UploadImage(c.Request.Context(), strings.TrimSpace(req.Path))
		if err != nil {
			logging.Warnf("upload image failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "upload image failed"})
			return
		}
		imageKey = strings.TrimSpace(uploaded)
	}
	if imageKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "image_key or path is required"})
		return
	}
	if err := s.dispatchImage(c.Request.Context(), session, imageKey); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		if err := s.dispatchText(c.Request.Context(), session, caption); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "type": "image", "image_key": imageKey})
}

func (s *Server) handleSendFile(c *gin.Context) {
	if !s.allowRuntimeMessage() {
		c.JSON(http.StatusForbidden, gin.H{"error": "runtime message is disabled for this bot"})
		return
	}
	if s.sender == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sender is unavailable"})
		return
	}
	session, err := sessionContextFromHeaders(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	var req FileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fileKey := strings.TrimSpace(req.FileKey)
	if fileKey == "" {
		if err := validatePathUnderRoot(req.Path, session.ResourceRoot); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		uploaded, err := s.sender.UploadFile(c.Request.Context(), strings.TrimSpace(req.Path), strings.TrimSpace(req.FileName))
		if err != nil {
			logging.Warnf("upload file failed: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "upload file failed"})
			return
		}
		fileKey = strings.TrimSpace(uploaded)
	}
	if fileKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_key or path is required"})
		return
	}
	if err := s.dispatchFile(c.Request.Context(), session, fileKey); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if caption := strings.TrimSpace(req.Caption); caption != "" {
		if err := s.dispatchText(c.Request.Context(), session, caption); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "type": "file", "file_key": fileKey})
}
