// Package a holds a type that shares its name with one in package b.
package a

// Pet collides with b.Pet.
type Pet struct {
	Name string `json:"name"`
}
