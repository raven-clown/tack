// Package b holds a type that shares its name with one in package a.
package b

// Pet collides with a.Pet.
type Pet struct {
	Legs int `json:"legs"`
}
