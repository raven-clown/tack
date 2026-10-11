package bench

import (
	"net/http"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

type echoListQuery struct {
	Kind  string `query:"kind" validate:"omitempty,oneof=cat dog bird"`
	Limit int    `query:"limit" validate:"min=1,max=100"`
}

type echoID struct {
	ID int `param:"id" validate:"min=1"`
}

type echoPetInput struct {
	Name string `json:"name" validate:"required,max=50"`
	Kind string `json:"kind" validate:"required,oneof=cat dog bird"`
}

type echoUpdate struct {
	ID   int    `param:"id" validate:"min=1"`
	Name string `json:"name" validate:"required,max=50"`
	Kind string `json:"kind" validate:"required,oneof=cat dog bird"`
}

type echoValidator struct{ v *validator.Validate }

func (e echoValidator) Validate(i any) error {
	if err := e.v.Struct(i); err != nil {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, err.Error())
	}
	return nil
}

// NewEcho uses Echo's Bind for the path, the query, and the body, and the
// validator Echo's documentation shows (go-playground/validator behind
// echo.Validator). Bind errors are 400 and validation errors 422; Echo has
// no default values, so the handler sets limit before binding.
func NewEcho() http.Handler {
	e := echo.New()
	e.Validator = echoValidator{validator.New()}
	e.GET("/pets", func(c echo.Context) error {
		q := echoListQuery{Limit: 20}
		if err := c.Bind(&q); err != nil {
			return err
		}
		if err := c.Validate(&q); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, listPets(q.Kind, q.Limit))
	})
	e.GET("/pets/:id", func(c echo.Context) error {
		var in echoID
		if err := c.Bind(&in); err != nil {
			return err
		}
		if err := c.Validate(&in); err != nil {
			return err
		}
		p, ok := getPet(in.ID)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "pet not found")
		}
		return c.JSON(http.StatusOK, p)
	})
	e.POST("/pets", func(c echo.Context) error {
		var in echoPetInput
		if err := c.Bind(&in); err != nil {
			return err
		}
		if err := c.Validate(&in); err != nil {
			return err
		}
		return c.JSON(http.StatusCreated, createPet(in.Name, in.Kind))
	})
	e.PUT("/pets/:id", func(c echo.Context) error {
		var in echoUpdate
		if err := c.Bind(&in); err != nil {
			return err
		}
		if err := c.Validate(&in); err != nil {
			return err
		}
		p, ok := updatePet(in.ID, in.Name, in.Kind)
		if !ok {
			return echo.NewHTTPError(http.StatusNotFound, "pet not found")
		}
		return c.JSON(http.StatusOK, p)
	})
	return e
}
