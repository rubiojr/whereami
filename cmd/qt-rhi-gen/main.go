// Command qt-rhi-gen runs MIQT's existing Clang-based binding generator with a
// focused Qt RHI allowlist. Generated adapters contain no map rendering logic.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed config.go.txt
var configuration string

func main() {
	out := flag.String("out", "internal/qtrhi", "generated binding directory")
	include := flag.String("rhi-include", "", "QtGui private include directory containing rhi/qrhi.h")
	flag.Parse()
	if err := run(*out, *include); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out, include string) error {
	if include == "" {
		return fmt.Errorf("-rhi-include must name the matching QtGui private include directory")
	}
	include, err := filepath.Abs(include)
	if err != nil {
		return err
	}
	versionData, err := exec.Command("pkg-config", "--modversion", "Qt6Gui").Output()
	if err != nil {
		return err
	}
	version := strings.TrimSpace(string(versionData))
	if filepath.Base(filepath.Dir(include)) != version {
		return fmt.Errorf("RHI headers must match Qt %s (got %s)", version, include)
	}
	if _, err := os.Stat(filepath.Join(include, "rhi", "qrhi.h")); err != nil {
		return err
	}
	out, err = filepath.Abs(out)
	if err != nil {
		return err
	}
	data, err := exec.Command("go", "mod", "download", "-json", "github.com/mappu/miqt@v0.14.0").Output()
	if err != nil {
		return err
	}
	var module struct{ Dir string }
	if err := json.Unmarshal(data, &module); err != nil {
		return err
	}
	temp, err := os.MkdirTemp("", "whereami-rhigen-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	source := filepath.Join(module.Dir, "cmd", "genbindings")
	if err := prepareGenerator(source, temp); err != nil {
		return err
	}
	if err := executeGenerator(temp, include); err != nil {
		return err
	}
	return installGenerated(filepath.Join(temp, "qtrhi"), out, version)
}

func prepareGenerator(source, temp string) error {
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			return err
		}
		text := patchGenerator(name, string(data))
		if err := os.WriteFile(filepath.Join(temp, name), []byte(text), 0644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(temp, "go.mod"), []byte("module rhigen\n\ngo 1.25.0\n"), 0644); err != nil {
		return err
	}
	return os.Mkdir(filepath.Join(temp, "cachedir"), 0755)
}

func patchGenerator(name, text string) string {
	switch name {
	case "config-libraries.go":
		text = configuration
	case "config-allowlist.go":
		text = regexp.MustCompile(`(?m)^\s*"QRhi",[^\n]*\n`).ReplaceAllString(text, "")
	case "main.go":
		text = strings.Replace(text, "astTransformApplyQuirks(packageName, parsed)", "rhiFilter(parsed); astTransformApplyQuirks(packageName, parsed)", 1)
	case "clangfilter.go":
		text = strings.ReplaceAll(text, `log.Printf("clangfilter:`, `rhiLog("clangfilter:`)
		text = strings.Replace(text, "\t\"log\"\n", "", 1)
	case "emitcabi.go":
		text = strings.Replace(text, `return preamble, nameprefix + "_QPair"`, `if p.Pointer { return preamble, "&" + nameprefix + "_QPair" }; return preamble, nameprefix + "_QPair"`, 1)
		// Generic lifecycle notifications: release callback handles when Qt
		// destroys a generated subclass, including non-QObject subclasses.
		text = strings.Replace(text, "for _, c := range src.Classes {\n\t\tclassName := cabiClassName(c.ClassName)", `ret.WriteString("extern \"C\" void qtrhi_native_destroyed(void*);\nextern \"C\" void qtrhi_callback_released(intptr_t);\n")
			for _, c := range src.Classes {
			className := cabiClassName(c.ClassName)`, 1)
		text = strings.Replace(text, `"\tvirtual ~" + subclassName + "() override = default;\n"`, `"\tvirtual ~" + subclassName + "() override { qtrhi_native_destroyed(this); " + rhiReleaseHandles(virtualMethods) + " }\n"`, 1)
	case "emitgo.go":
		text = strings.ReplaceAll(text, `ok := C.`+"` + cabiOverrideVirtualName(c, m) + `"+`(unsafe.Pointer(this.h), C.intptr_t(cgo.NewHandle(slot)) )`, `handle := cgo.NewHandle(slot)
			ok := C.`+"` + cabiOverrideVirtualName(c, m) + `"+`(unsafe.Pointer(this.h), C.intptr_t(handle) )`)
		text = strings.ReplaceAll(text, `panic("miqt: can only override virtual methods for directly constructed types")`, `handle.Delete(); panic("miqt: can only override virtual methods for directly constructed types")`)
	}
	return text
}

func executeGenerator(temp, include string) error {
	command := exec.Command("go", "run", ".", "-outdir", temp)
	command.Dir = temp
	command.Env = append(os.Environ(), "QT_RHI_INCLUDE="+include)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("MIQT generator: %w\n%s", err, output.String())
	}
	return nil
}

func installGenerated(source, out, version string) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	names, err := generatedSymbols(source, entries)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		data, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			return err
		}
		text, err := rewriteGenerated(name, string(data), version, names)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, name), []byte(text), 0644); err != nil {
			return err
		}
	}
	return nil
}

func generatedSymbols(source string, entries []os.DirEntry) ([]string, error) {
	// Namespace C ABI symbols so the experimental bindings can coexist with
	// the existing MIQT subset during side-by-side comparisons.
	symbols := map[string]bool{}
	functions := regexp.MustCompile(`(?m)^[^\n#]*?\b([A-Za-z_][A-Za-z0-9_]*)\([^;\n]*\);`)
	callbacks := regexp.MustCompile(`(?m)^\s*//export ([A-Za-z_][A-Za-z0-9_]*)`)
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(source, entry.Name()))
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(entry.Name(), ".h") {
			for _, m := range functions.FindAllStringSubmatch(string(data), -1) {
				symbols[m[1]] = true
			}
		}
		if strings.HasSuffix(entry.Name(), ".go") {
			for _, m := range callbacks.FindAllStringSubmatch(string(data), -1) {
				symbols[m[1]] = true
			}
		}
	}
	names := make([]string, 0, len(symbols))
	for name := range symbols {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func rewriteGenerated(name, text, version string, names []string) (string, error) {
	for _, symbol := range names {
		if strings.HasSuffix(name, ".go") {
			text = strings.ReplaceAll(text, "C."+symbol+"(", "C.qtrhi_"+symbol+"(")
			text = strings.ReplaceAll(text, "func "+symbol+"(", "func qtrhi_"+symbol+"(")
			text = strings.ReplaceAll(text, "//export "+symbol+"\n", "//export qtrhi_"+symbol+"\n")
		} else {
			text = regexp.MustCompile(`\b`+regexp.QuoteMeta(symbol)+`\b`).ReplaceAllString(text, "qtrhi_"+symbol)
		}
	}
	if name == "gen_qrhi.cpp" {
		text += "\nstatic_assert(QT_VERSION == QT_VERSION_CHECK(" + strings.ReplaceAll(version, ".", ",") + "), \"Regenerate QRhi bindings for this Qt version\");\n"
	}
	if name == "gen_qrhi.go" {
		text += "\n// QtVersion is the exact Qt SDK version used for generation.\nconst QtVersion = " + fmt.Sprintf("%q", version) + "\n"
	}
	text = strings.ReplaceAll(text, `"../libmiqt/libmiqt.h"`, `"../../vendor/github.com/mappu/miqt/libmiqt/libmiqt.h"`)
	if strings.HasSuffix(name, ".cpp") {
		text = regexp.MustCompile(`(if \(self_cast == nullptr\) \{\n\s*return false;\n\s*\}\n\n\s*)(self_cast->handle__([A-Za-z0-9_]+) = slot;)`).ReplaceAllString(text, "${1}if (self_cast->handle__${3} != 0) return false;\n\t${2}")
		text = regexp.MustCompile(`#include <QRhi[^>]*>`).ReplaceAllString(text, "#include <rhi/qrhi.h>")
		text = strings.ReplaceAll(text, "#include <qrhi.h>", "#include <rhi/qrhi.h>")
		text = strings.ReplaceAll(text, "#include <QShader>", "#include <rhi/qshader.h>")
		text = strings.ReplaceAll(text, "#include <qshader.h>", "#include <rhi/qshader.h>")
		// QRhi exposes initializer-list setters together with iterator
		// overloads. MIQT marshals these as QList; call the iterator form.
		for _, method := range []string{"setBindings", "setAttributes", "setShaderStages", "setTargetBlends", "setEntries"} {
			text = strings.ReplaceAll(text, method+"(list_QList)", method+"(list_QList.cbegin(), list_QList.cend())")
		}
	}
	if strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".cpp") {
		text = "//go:build vecmap_rhi\n\n// Code generated by qt-rhi-gen using MIQT v0.14.0; DO NOT EDIT.\n" + text
	}
	if strings.HasSuffix(name, ".go") {
		// MIQT projects a pointer-to-QPair as one pair, not an array. Reject
		// larger counts before crossing the ABI; this backend uses one stream
		// and one dynamic uniform offset. The no-pointer overload requires zero.
		text = regexp.MustCompile(`(?s)(func \(this \*QRhiCommandBuffer\) SetVertexInput\w*\(.*?\) \{\n)`).ReplaceAllString(text, "${1}if bindingCount != 1 { panic(\"qtrhi: expected one vertex binding\") }\n")
		text = regexp.MustCompile(`(?s)(func \(this \*QRhiCommandBuffer\) SetShaderResources3\(.*?\) \{\n)`).ReplaceAllString(text, "${1}if dynamicOffsetCount != 1 { panic(\"qtrhi: expected one dynamic offset\") }\n")
		text = regexp.MustCompile(`(?s)(func \(this \*QRhiCommandBuffer\) SetShaderResources2\(.*?\) \{\n)`).ReplaceAllString(text, "${1}if dynamicOffsetCount != 0 { panic(\"qtrhi: no offset array supplied\") }\n")
		// Native GPU objects and returned value copies are explicitly released
		// on their owning thread. Runtime finalizers cannot do that safely.
		text = regexp.MustCompile(`(?m)^\s*_goptr\.GoGC\(\)[^\n]*\n`).ReplaceAllString(text, "")
		formatted, err := format.Source([]byte(text))
		if err != nil {
			return "", err
		}
		text = string(formatted)
	}
	return strings.TrimRight(text, "\n") + "\n", nil
}
