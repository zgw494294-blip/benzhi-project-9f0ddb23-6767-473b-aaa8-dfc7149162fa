package domain

type Bounds struct {
	MinX float64 `json:"minX"`
	MinY float64 `json:"minY"`
	MaxX float64 `json:"maxX"`
	MaxY float64 `json:"maxY"`
}

func (b Bounds) Contains(c Coordinate) bool {
	return c.X >= b.MinX && c.X <= b.MaxX && c.Y >= b.MinY && c.Y <= b.MaxY
}

type Coordinate struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}
