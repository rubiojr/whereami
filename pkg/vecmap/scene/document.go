package scene

// Document is a portable, offline rendering fixture. It can be consumed by any
// backend without importing the Qt-bound vecmap package that produced it.
type Document struct {
	Scene         Scene
	Transforms    []Affine
	Width, Height int
	Labels        int
	MissingFonts  []string
	Source        string
}
