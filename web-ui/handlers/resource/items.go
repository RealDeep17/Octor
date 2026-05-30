package resource

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/webtor-io/web-ui/services/api"
)

// getItems retrieves the complete list of files for a resource up to a high limit (10,000 files)
// and returns it in JSON format for client-side rendering and search.
func (s *Handler) getItems(c *gin.Context) {
	id := c.Param("resource_id")
	claims := api.GetClaimsFromContext(c)

	list, err := s.api.ListResourceContentCached(c.Request.Context(), claims, id, &api.ListResourceContentArgs{
		Output: api.OutputList,
		Limit:  10000,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if list == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "resource not found"})
		return
	}

	// Filter files specifically, matching the original layout form logic
	var files []any
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
