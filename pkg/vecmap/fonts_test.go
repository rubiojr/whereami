package vecmap

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmbeddedLibertyFontsArePinned(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		checksum string
	}{
		{name: "regular", data: libertyRegularFont, checksum: "7c4b26766a5858412e48af528155df4684ef35ceb44e95b1c6019f89927d90d8"},
		{name: "bold", data: libertyBoldFont, checksum: "a699728d556ec682775498c77f52017c553c17f7db3217ae3fcb7b8089abeeed"},
		{name: "italic", data: libertyItalicFont, checksum: "051a6fccd7f99b792ea21d209a3bb23485543c703c3da2d8b082409bab7efe74"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.NotEmpty(t, test.data)
			assert.Equal(t, test.checksum, fmt.Sprintf("%x", sha256.Sum256(test.data)))
		})
	}
}

func TestEmbeddedLibertyFontsRetainLicenseMetadata(t *testing.T) {
	fonts := [][]byte{libertyRegularFont, libertyBoldFont, libertyItalicFont}
	notices := [][]byte{
		[]byte("Copyright 2012 Google Inc. All Rights Reserved."),
		[]byte("Noto is a trademark of Google Inc."),
		[]byte("SIL Open Font License, Version 1.1"),
	}
	for _, font := range fonts {
		for _, notice := range notices {
			assert.True(t, bytes.Contains(font, notice), "font is missing metadata notice %q", notice)
		}
	}
}
