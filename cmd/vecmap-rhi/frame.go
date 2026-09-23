//go:build vecmap_rhi

package main

import (
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/scene"
	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

// streamUpdate is one immutable GUI-to-render-thread publication. The target is
// prepared off-thread; the camera remains independent of its transform slots.
type streamUpdate struct {
	target *scene.Document
	camera streamCamera
}

type streamCamera struct {
	geographic *view.Camera
	affine     scene.Affine
	dpr        float32
}

func (c streamCamera) frame(document *scene.Document) scene.Frame {
	if document == nil {
		return scene.Frame{DevicePixelRatio: c.dpr}
	}
	if c.geographic != nil && len(document.TileSpaces) > 0 {
		frame := document.FrameAt(*c.geographic)
		frame.DevicePixelRatio = c.dpr
		return frame
	}
	transforms := make([]scene.Affine, len(document.Transforms))
	for i, base := range document.Transforms {
		a := c.affine
		transforms[i] = scene.Affine{
			M11: a.M11*base.M11 + a.M12*base.M21, M12: a.M11*base.M12 + a.M12*base.M22,
			M21: a.M21*base.M11 + a.M22*base.M21, M22: a.M21*base.M12 + a.M22*base.M22,
			DX: a.M11*base.DX + a.M12*base.DY + a.DX, DY: a.M21*base.DX + a.M22*base.DY + a.DY,
		}
	}
	return scene.Frame{Scene: &document.Scene, Transforms: transforms, DevicePixelRatio: c.dpr}
}

// The initial document defines the camera trace, even while target documents
// change. Geographic target slots are projected with this same current camera.
func traceCamera(document scene.Document, t float64, animate bool) streamCamera {
	result := streamCamera{affine: scene.Affine{M11: 1, M22: 1}, dpr: 1}
	if document.Camera != nil && len(document.TileSpaces) > 0 {
		camera := document.Camera.WithViewport(float64(document.Width), float64(document.Height))
		if animate {
			camera.Zoom += 0.2 * math.Sin(t)
			camera.Bearing += 0.15 * math.Sin(t*0.7) * 180 / math.Pi
			camera = camera.Normalized().Panned(-math.Sin(t*1.3)*20, 0)
		}
		result.geographic = &camera
		return result
	}
	if animate {
		scale := float32(math.Exp2(0.2 * math.Sin(t)))
		sin, cos := math.Sincos(0.15 * math.Sin(t*0.7))
		a, b := float32(cos)*scale, float32(sin)*scale
		cx, cy := float32(document.Width)/2, float32(document.Height)/2
		result.affine = scene.Affine{M11: a, M12: -b, M21: b, M22: a,
			DX: cx - a*cx + b*cy + float32(math.Sin(t*1.3))*20, DY: cy - b*cx - a*cy}
	}
	return result
}
