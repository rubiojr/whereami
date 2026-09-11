//go:build integration

package quick

import (
	"runtime"
	"testing"
	"time"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/qml"
	"github.com/stretchr/testify/require"
)

// Exercise the real native allocation/Go buffer-write boundary on Qt's render
// thread, including two-pass ownership and destruction. Nodes are destroyed
// before rendering, so this works without a GPU or compiled shader resources.
func TestRenderBufferBridge(t *testing.T) {
	withRenderItem(t, func(item *QQuickItem) {
		points := []float32{0, 0, 8, 0, 0, 8}
		pattern := NewQSGPatternNode(item, points, []byte{255, 0, 0, 255}, 1, 1, 8, 8, 2, 3, 1)
		require.NotNil(t, pattern)
		require.Equal(t, 1, pattern.ChildCount())
		pattern.Delete()

		atlas := NewQSGSDFAtlas(item, []byte{0, 64, 128, 255}, 2, 2)
		require.NotNil(t, atlas)
		defer atlas.QSGNode().Delete()
		vertices := []float32{0, 0, 0, 0, 8, 0, 1, 0, 0, 8, 0, 1, 8, 0, 1, 0, 8, 8, 1, 1, 0, 8, 0, 1}
		for _, width := range []float32{0, 1} {
			node := atlas.NewTextNode(vertices, [4]int{255, 0, 0, 255}, [4]int{255, 255, 255, 255}, 1, width, 0)
			require.NotNil(t, node)
			require.Equal(t, int(width)*2, node.ChildCount())
			runtime.GC()
			node.Delete()
		}
	})
}

func BenchmarkPatternBufferBridge(b *testing.B) {
	withRenderItem(b, func(item *QQuickItem) {
		points := make([]float32, 6144*2)
		pixels := []byte{255, 255, 255, 255}
		b.ReportAllocs()
		for b.Loop() {
			node := NewQSGPatternNode(item, points, pixels, 1, 1, 37, 19, 3, 7, 1)
			if node == nil {
				b.Fatal("pattern creation failed")
			}
			node.Delete()
		}
	})
}

func BenchmarkSDFFillBufferBridge(b *testing.B) { benchmarkSDFBufferBridge(b, 0) }
func BenchmarkSDFHaloBufferBridge(b *testing.B) { benchmarkSDFBufferBridge(b, 1) }

func benchmarkSDFBufferBridge(b *testing.B, width float32) {
	withRenderItem(b, func(item *QQuickItem) {
		atlas := NewQSGSDFAtlas(item, []byte{0, 64, 128, 255}, 2, 2)
		require.NotNil(b, atlas)
		defer atlas.QSGNode().Delete()
		vertices := make([]float32, 24*32)
		b.ReportAllocs()
		for b.Loop() {
			node := atlas.NewTextNode(vertices, [4]int{255, 0, 0, 255}, [4]int{255, 255, 255, 255}, 1, width, 0)
			if node == nil {
				b.Fatal("text creation failed")
			}
			node.Delete()
		}
	})
}

func withRenderItem(tb testing.TB, run func(*QQuickItem)) {
	tb.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	tb.Setenv("QT_QUICK_BACKEND", "software")
	tb.Setenv("QSG_RENDER_LOOP", "basic")
	app := qt.NewQApplication([]string{"render-buffer-test", "-platform", "offscreen"})
	defer app.Delete()
	item := NewQQuickItem()
	defer item.Delete()
	item.SetFlag(QQuickItem__ItemHasContents)
	done := false
	item.OnUpdatePaintNode(func(_ func(*QSGNode, *QQuickItem__UpdatePaintNodeData) *QSGNode, old *QSGNode, _ *QQuickItem__UpdatePaintNodeData) *QSGNode {
		if !done {
			done = true
			run(item)
		}
		return old
	})
	engine := qml.NewQQmlApplicationEngine()
	defer engine.Delete()
	engine.RootContext().SetContextProperty("testItem", item.QObject)
	engine.LoadData([]byte(`import QtQuick
import QtQuick.Window
Window {
    visible: true; width: 32; height: 32
    Item {
        id: host; anchors.fill: parent
        Binding { target: testItem; property: "parent"; value: host }
        Binding { target: testItem; property: "width"; value: host.width }
        Binding { target: testItem; property: "height"; value: host.height }
    }
}`))
	require.Len(tb, engine.RootObjects(), 1)
	deadline := time.Now().Add(5 * time.Second)
	for !done && time.Now().Before(deadline) {
		qt.QCoreApplication_ProcessEvents()
		time.Sleep(time.Millisecond)
	}
	require.True(tb, done, "render callback did not run")
}
