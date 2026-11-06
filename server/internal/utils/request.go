package utils

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

func ParsePagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "10"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	return
}

func ParseID(c *gin.Context) (uint, error) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		return 0, err
	}
	return uint(id), nil
}

func ParseUintQuery(c *gin.Context, key string) (uint, error) {
	id, err := strconv.Atoi(c.Query(key))
	if err != nil || id <= 0 {
		return 0, err
	}
	return uint(id), nil
}
