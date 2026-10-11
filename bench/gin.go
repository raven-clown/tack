package bench

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type ginListQuery struct {
	Kind  string `form:"kind" binding:"omitempty,oneof=cat dog bird"`
	Limit int    `form:"limit,default=20" binding:"min=1,max=100"`
}

type ginID struct {
	ID int `uri:"id" binding:"min=1"`
}

type ginPetInput struct {
	Name string `json:"name" binding:"required,max=50"`
	Kind string `json:"kind" binding:"required,oneof=cat dog bird"`
}

// NewGin uses Gin's binding (go-playground/validator underneath) for the
// query, the path, and the body, in release mode without middleware. Gin's
// convention is 400 for every binding and validation error.
func NewGin() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/pets", func(c *gin.Context) {
		var q ginListQuery
		if err := c.ShouldBindQuery(&q); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, listPets(q.Kind, q.Limit))
	})
	r.GET("/pets/:id", func(c *gin.Context) {
		var in ginID
		if err := c.ShouldBindUri(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		p, ok := getPet(in.ID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "pet not found"})
			return
		}
		c.JSON(http.StatusOK, p)
	})
	r.POST("/pets", func(c *gin.Context) {
		var in ginPetInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, createPet(in.Name, in.Kind))
	})
	r.PUT("/pets/:id", func(c *gin.Context) {
		var id ginID
		if err := c.ShouldBindUri(&id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var in ginPetInput
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		p, ok := updatePet(id.ID, in.Name, in.Kind)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "pet not found"})
			return
		}
		c.JSON(http.StatusOK, p)
	})
	return r
}
