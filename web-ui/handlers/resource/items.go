package resource

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Handler) getItems(c *gin.Context) {
	id := c.Param("resource_id")
	claims := s.api.GetClaimsFromContext(c)

	list, err := s.api.GetResourceItemsCached(c.Request.Context(), claims, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get resource items"})
		return
	}
	if list == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	// Filter files specifically, matching the original layout form logic.
	// Initialize as empty slice (not nil) so it serializes to JSON [] instead of null.
	files := make([]any, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Type == "file" {
			files = append(files, item)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"items":       files,
		"size":        list.Size,
		"resource_id": id,
	})
}
