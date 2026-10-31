package shell

import (
	"github.com/gin-gonic/gin"

	bulkids "go_wp/pkg/bulkids"
)

func BulkIDs(c *gin.Context) ([]string, error) {
	return bulkids.Normalize(c.PostFormArray("ids"))
}
