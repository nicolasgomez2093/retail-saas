package storefront

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"sync"
)

// shrink reduces a JPEG photo to fit in a side×side square, averaging the pixels of each block so
// fabrics and prints stay sharp. Smaller photos are returned as they are.
func shrink(source []byte, side int) ([]byte, error) {
	picture, err := jpeg.Decode(bytes.NewReader(source))
	if err != nil {
		return nil, err
	}
	bounds := picture.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= side && height <= side {
		return source, nil
	}
	targetWidth, targetHeight := side, height*side/width
	if height > width {
		targetWidth, targetHeight = width*side/height, side
	}
	targetWidth, targetHeight = max(targetWidth, 1), max(targetHeight, 1)
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), picture, bounds.Min, draw.Src)
	output := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := range targetHeight {
		top, bottom := y*height/targetHeight, max((y+1)*height/targetHeight, y*height/targetHeight+1)
		for x := range targetWidth {
			left, right := x*width/targetWidth, max((x+1)*width/targetWidth, x*width/targetWidth+1)
			var red, green, blue, count int
			for sy := top; sy < bottom; sy++ {
				row := rgba.Pix[sy*rgba.Stride+left*4 : sy*rgba.Stride+right*4]
				for i := 0; i < len(row); i += 4 {
					red, green, blue = red+int(row[i]), green+int(row[i+1]), blue+int(row[i+2])
					count++
				}
			}
			offset := y*output.Stride + x*4
			output.Pix[offset], output.Pix[offset+1], output.Pix[offset+2], output.Pix[offset+3] =
				uint8(red/count), uint8(green/count), uint8(blue/count), 255
		}
	}
	var result bytes.Buffer
	if err := jpeg.Encode(&result, output, &jpeg.Options{Quality: 82}); err != nil {
		return nil, err
	}
	return result.Bytes(), nil
}

// imageCache keeps reduced photos up to a byte budget, dropping the oldest first.
type imageCache struct {
	mu     sync.Mutex
	budget int
	used   int
	order  []string
	items  map[string][]byte
}

func newImageCache(budget int) *imageCache {
	return &imageCache{budget: budget, items: map[string][]byte{}}
}

func (c *imageCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	body, ok := c.items[key]
	return body, ok
}

func (c *imageCache) put(key string, body []byte) {
	if len(body) > c.budget/4 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; ok {
		return
	}
	for c.used+len(body) > c.budget && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		c.used -= len(c.items[oldest])
		delete(c.items, oldest)
	}
	c.items[key] = body
	c.order = append(c.order, key)
	c.used += len(body)
}
