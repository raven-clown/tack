// Command gin-swaggo is the Gin + swaggo variant of the comparison API.
package main

import (
	"net/http"
	"sort"
	"strconv"
	"sync"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "ginswaggo/docs"
)

// Pet is a pet in the store.
type Pet struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind" enums:"cat,dog,bird"`
}

// PetInput is the writable part of a Pet.
type PetInput struct {
	Name string `json:"name" binding:"required,max=50" maxLength:"50"`
	Kind string `json:"kind" binding:"required,oneof=cat dog bird" enums:"cat,dog,bird"`
}

// Problem describes an error.
type Problem struct {
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

type listQuery struct {
	Kind  string `form:"kind" binding:"omitempty,oneof=cat dog bird"`
	Limit int    `form:"limit,default=20" binding:"min=1,max=100"`
}

type store struct {
	mu   sync.Mutex
	pets map[int]Pet
	next int
}

// @title Pet Store
// @version 1.0.0
func main() {
	s := &store{pets: map[int]Pet{}}
	r := gin.New()
	r.GET("/pets", s.list)
	r.GET("/pets/:id", s.get)
	r.POST("/pets", s.create)
	r.PUT("/pets/:id", s.update)
	r.GET("/docs/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	_ = r.Run(":8080")
}

// list returns pets.
//
//	@Summary	List pets
//	@Param		kind	query		string	false	"Only pets of this kind"	Enums(cat, dog, bird)
//	@Param		limit	query		int		false	"Maximum number of pets"	default(20)	minimum(1)	maximum(100)
//	@Success	200		{array}		Pet
//	@Failure	400		{object}	Problem
//	@Router		/pets [get]
func (s *store) list(c *gin.Context) {
	var q listQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		c.JSON(http.StatusBadRequest, Problem{http.StatusBadRequest, err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pets := make([]Pet, 0, len(s.pets))
	for _, p := range s.pets {
		if q.Kind == "" || p.Kind == q.Kind {
			pets = append(pets, p)
		}
	}
	sort.Slice(pets, func(i, j int) bool { return pets[i].ID < pets[j].ID })
	if len(pets) > q.Limit {
		pets = pets[:q.Limit]
	}
	c.JSON(http.StatusOK, pets)
}

// get returns one pet.
//
//	@Summary	Get a pet
//	@Param		id	path		int	true	"Pet ID"
//	@Success	200	{object}	Pet
//	@Failure	400	{object}	Problem
//	@Failure	404	{object}	Problem
//	@Router		/pets/{id} [get]
func (s *store) get(c *gin.Context) {
	id, ok := petID(c)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p, found := s.pets[id]
	if !found {
		c.JSON(http.StatusNotFound, Problem{http.StatusNotFound, "pet not found"})
		return
	}
	c.JSON(http.StatusOK, p)
}

// create adds a pet.
//
//	@Summary	Create a pet
//	@Param		pet	body		PetInput	true	"Pet"
//	@Success	201	{object}	Pet
//	@Failure	400	{object}	Problem
//	@Router		/pets [post]
func (s *store) create(c *gin.Context) {
	var in PetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, Problem{http.StatusBadRequest, err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	p := Pet{ID: s.next, Name: in.Name, Kind: in.Kind}
	s.pets[p.ID] = p
	c.JSON(http.StatusCreated, p)
}

// update replaces a pet.
//
//	@Summary	Update a pet
//	@Param		id	path		int			true	"Pet ID"
//	@Param		pet	body		PetInput	true	"Pet"
//	@Success	200	{object}	Pet
//	@Failure	400	{object}	Problem
//	@Failure	404	{object}	Problem
//	@Router		/pets/{id} [put]
func (s *store) update(c *gin.Context) {
	id, ok := petID(c)
	if !ok {
		return
	}
	var in PetInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, Problem{http.StatusBadRequest, err.Error()})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.pets[id]; !found {
		c.JSON(http.StatusNotFound, Problem{http.StatusNotFound, "pet not found"})
		return
	}
	p := Pet{ID: id, Name: in.Name, Kind: in.Kind}
	s.pets[id] = p
	c.JSON(http.StatusOK, p)
}

func petID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		c.JSON(http.StatusBadRequest, Problem{http.StatusBadRequest, "id must be a positive integer"})
		return 0, false
	}
	return id, true
}
