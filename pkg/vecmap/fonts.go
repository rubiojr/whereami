package vecmap

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	qt "github.com/mappu/miqt/qt6"
)

const libertyDesktopFontFamily = "KlokanTech Noto Sans"

var (
	//go:embed fonts/KlokanTechNotoSans-Regular.ttf
	libertyRegularFont []byte
	//go:embed fonts/KlokanTechNotoSans-Bold.ttf
	libertyBoldFont []byte
	//go:embed fonts/KlokanTechNotoSans-Italic.ttf
	libertyItalicFont []byte

	libertyFontRegistrationMu sync.Mutex
	libertyFontsRegistered    bool
	libertyFontApplication    uintptr
)

// RegisterFonts installs the pinned OpenMapTiles Noto faces in Qt's application
// font database. It must run on the GUI thread after QGuiApplication exists.
func RegisterFonts() error {
	libertyFontRegistrationMu.Lock()
	defer libertyFontRegistrationMu.Unlock()
	application := qt.QCoreApplication_Instance()
	if application == nil || application.Metacast("QGuiApplication") == nil {
		return fmt.Errorf("register Liberty fonts: QGuiApplication is not initialized")
	}
	if application.Thread().UnsafePointer() != qt.QThread_CurrentThread().UnsafePointer() {
		return fmt.Errorf("register Liberty fonts: must run on the GUI thread")
	}
	applicationPointer := uintptr(application.UnsafePointer())
	if libertyFontsRegistered && libertyFontApplication == applicationPointer {
		return nil
	}

	fonts := []struct {
		name string
		data []byte
	}{
		{name: "regular", data: libertyRegularFont},
		{name: "bold", data: libertyBoldFont},
		{name: "italic", data: libertyItalicFont},
	}
	registeredIDs := make([]int, 0, len(fonts))
	for _, font := range fonts {
		id := qt.QFontDatabase_AddApplicationFontFromData(font.data)
		if id < 0 {
			removeApplicationFonts(registeredIDs)
			return fmt.Errorf("register Liberty %s font: QFontDatabase rejected the embedded data", font.name)
		}
		registeredIDs = append(registeredIDs, id)
		if !containsString(qt.QFontDatabase_ApplicationFontFamilies(id), libertyDesktopFontFamily) {
			removeApplicationFonts(registeredIDs)
			return fmt.Errorf("liberty %s font has unexpected family", font.name)
		}
	}
	libertyFontsRegistered = true
	libertyFontApplication = applicationPointer
	application.QObject.OnDestroyed(func() {
		libertyFontRegistrationMu.Lock()
		if libertyFontApplication == applicationPointer {
			libertyFontsRegistered = false
			libertyFontApplication = 0
		}
		libertyFontRegistrationMu.Unlock()
	})
	return nil
}

func removeApplicationFonts(ids []int) {
	for _, id := range ids {
		qt.QFontDatabase_RemoveApplicationFont(id)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func libertyDesktopFont(font string) string {
	if font == "Noto Sans" {
		return libertyDesktopFontFamily
	}
	if suffix, found := strings.CutPrefix(font, "Noto Sans "); found {
		return libertyDesktopFontFamily + " " + suffix
	}
	return font
}
