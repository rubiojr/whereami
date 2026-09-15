package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/liberty"

type libertySpriteImage struct {
	pixels        []byte
	width, height int
	pixelRatio    float64
}

func libertySprite(name string, color mapColor, opacity float64) (libertySpriteImage, bool) {
	image, ok := liberty.Sprite(name, color, opacity)
	return libertySpriteImage{pixels: image.Pixels, width: image.Width, height: image.Height, pixelRatio: image.PixelRatio}, ok
}
