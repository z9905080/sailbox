package v1

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sailboxhq/sailbox/apps/api/internal/api/middleware"
	"github.com/sailboxhq/sailbox/apps/api/internal/apierr"
	"github.com/sailboxhq/sailbox/apps/api/internal/httputil"
	"github.com/sailboxhq/sailbox/apps/api/internal/service"
	"time"
)

type APIKeyHandler struct{ svc *service.APIKeyService }

func NewAPIKeyHandler(svc *service.APIKeyService) *APIKeyHandler { return &APIKeyHandler{svc: svc} }

type apiKeyInput struct {
	Name      string     `json:"name" binding:"required,max=100"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (h *APIKeyHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	keys, err := h.svc.List(c.Request.Context(), middleware.GetUserID(c))
	if err != nil {
		httputil.RespondError(c, err)
		return
	}
	httputil.RespondList(c, keys)
}
func (h *APIKeyHandler) Create(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var input apiKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httputil.RespondError(c, apierr.ErrValidation.WithDetail("invalid name or expiry"))
		return
	}
	key, plaintext, err := h.svc.Create(c.Request.Context(), middleware.GetUserID(c), input.Name, input.ExpiresAt)
	if err != nil {
		httputil.RespondError(c, err)
		return
	}
	httputil.RespondCreated(c, gin.H{"api_key": key, "key": plaintext}, "")
}
func (h *APIKeyHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.RespondError(c, apierr.ErrBadRequest.WithDetail("invalid key ID"))
		return
	}
	var input apiKeyInput
	if err := c.ShouldBindJSON(&input); err != nil {
		httputil.RespondError(c, apierr.ErrValidation.WithDetail("invalid name or expiry"))
		return
	}
	if err := h.svc.Update(c.Request.Context(), id, middleware.GetUserID(c), input.Name, input.ExpiresAt); err != nil {
		httputil.RespondError(c, err)
		return
	}
	httputil.RespondNoContent(c)
}
func (h *APIKeyHandler) Revoke(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httputil.RespondError(c, apierr.ErrBadRequest.WithDetail("invalid key ID"))
		return
	}
	if err := h.svc.Revoke(c.Request.Context(), id, middleware.GetUserID(c)); err != nil {
		httputil.RespondError(c, err)
		return
	}
	httputil.RespondNoContent(c)
}
