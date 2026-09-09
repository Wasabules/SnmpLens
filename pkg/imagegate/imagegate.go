// Package imagegate is the gate a background image has to get through.
//
// A map widget draws over a picture the operator supplies, and that picture
// reaches this application from a file dialog and leaves it as a data URI in a
// WebView. Both ends are why this exists.
//
// The gate is POSITIVE, like the preset import gate and the MIB import gate: a
// file gets in only if the standard library DECODES it as one of three raster
// formats and its dimensions are sane. Not a check on the extension, which is
// what the operating system's file dialog filters on and what an attacker
// names their file; and not a blacklist, which is a list of the formats
// somebody thought of.
//
// The package is named for the gate rather than for the pictures: main.go
// already has an `assets` at package scope — the embedded frontend bundle — and
// a package called the same thing cannot be imported beside it at all.
//
// SVG is refused, and that is the whole reason this is a decode rather than a
// sniff. An <svg> is a document with script in it, and the one place this
// picture ends up is a WebView with a bridge to the operating system on the
// other side. There is no sanitiser worth trusting against a format designed to
// be extensible; pkg/preset's frozen shape vocabulary is the answer to what SVG
// would have been for.
//
// WebP is refused too, and that one is a trade rather than a danger: there is
// no decoder in the standard library, so accepting it means a dependency whose
// job is to parse hostile input. The site's own images are WebP and are made by
// a tool at build time; this is a file from a person's disk.
package imagegate

import (
	"bytes"
	"fmt"
	"image"
	"strings"

	// The three decoders this gate accepts, registered for image.DecodeConfig.
	// DecodeConfig reads the HEADER only — it does not allocate the pixels —
	// which is what makes it safe to run on a file before believing anything
	// about its size.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

const (
	// MaxBytes bounds one background.
	//
	// Four mebibytes, and the bound that matters is not the disk: the renderer
	// receives this as a base64 data URI, which is a third larger again, held
	// as a JavaScript string. A rack photograph is a few hundred kilobytes.
	MaxBytes = 4 << 20
	// MaxPixels bounds what a decoder would have to allocate if anything ever
	// decoded the whole image. 40 megapixels is past any photograph anybody
	// draws a rack on, and it is the difference between refusing a file and
	// discovering a decompression bomb in a WebView.
	MaxPixels = 40_000_000
	// MaxDimension refuses the other shape of the same trick: 1 x 200000000.
	MaxDimension = 20000
)

// Kinds are the formats this gate admits, as their canonical media types.
var mediaTypes = map[string]string{
	"png":  "image/png",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
}

// Info is what the gate learned about a file it accepted.
type Info struct {
	Format string `json:"format"`
	Media  string `json:"media"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int    `json:"bytes"`
}

// Inspect decides whether these bytes are an image this application will show.
//
// The format is what the DECODER says, never what the name says: a file called
// rack.png that decodes as a GIF is a GIF, and is served as one.
func Inspect(data []byte) (Info, error) {
	if len(data) == 0 {
		return Info{}, fmt.Errorf("the file is empty")
	}
	if len(data) > MaxBytes {
		return Info{}, fmt.Errorf("the file is %d bytes; at most %d", len(data), MaxBytes)
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// The four things people actually pick by mistake, named rather than
		// reported as "unknown format" — which is what an SVG, a PDF, an HTML
		// page and a zip all arrive as.
		if hint := looksLike(data); hint != "" {
			return Info{}, fmt.Errorf("this is %s, not an image this version can show (PNG, JPEG or GIF)", hint)
		}
		return Info{}, fmt.Errorf("this is not a PNG, a JPEG or a GIF")
	}
	media, ok := mediaTypes[format]
	if !ok {
		return Info{}, fmt.Errorf("%s images are not accepted here", format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Info{}, fmt.Errorf("the image has no size")
	}
	if cfg.Width > MaxDimension || cfg.Height > MaxDimension {
		return Info{}, fmt.Errorf("the image is %dx%d; at most %d on a side",
			cfg.Width, cfg.Height, MaxDimension)
	}
	if cfg.Width*cfg.Height > MaxPixels {
		return Info{}, fmt.Errorf("the image is %d megapixels; at most %d",
			cfg.Width*cfg.Height/1_000_000, MaxPixels/1_000_000)
	}

	return Info{
		Format: format, Media: media,
		Width: cfg.Width, Height: cfg.Height, Bytes: len(data),
	}, nil
}

// looksLike recognises what people download by mistake.
//
// The same idea as pkg/mib/diagnose.go's: an HTML page, a PDF, a zip and an SVG
// all reach a decoder as the same sentence, and saying which one it is turns a
// puzzle into a fixable mistake.
func looksLike(data []byte) string {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	lower := strings.ToLower(strings.TrimSpace(string(head)))

	switch {
	case strings.HasPrefix(lower, "%pdf-"):
		return "a PDF"
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		return "a zip archive"
	case strings.HasPrefix(lower, "<svg"), strings.Contains(lower, "<svg "):
		// Named rather than lumped in with "not an image", because it IS an
		// image and refusing it looks like a bug unless the reason is said.
		return "an SVG, which this application deliberately does not render"
	case strings.HasPrefix(lower, "<!doctype html"), strings.HasPrefix(lower, "<html"):
		return "an HTML page"
	case bytes.HasPrefix(head, []byte("RIFF")) && bytes.Contains(head, []byte("WEBP")):
		return "a WebP"
	}
	return ""
}

// ValidName says whether a name may be used for an asset.
//
// A LEAF, and nothing else: the assets directory sits beside mibs/ and
// presets/, one level under monitoring.db, service.json and the secret store.
// pkg/preset carries the same rule as a validation MESSAGE; this one is the
// enforcement, and neither trusts the other.
func ValidName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return fmt.Errorf("a background needs a name")
	}
	if trimmed != name {
		return fmt.Errorf("a name may not begin or end with a space")
	}
	if len(name) > 120 {
		return fmt.Errorf("the name is %d characters; at most 120", len(name))
	}
	if strings.ContainsAny(name, `/\:`) || strings.Contains(name, "..") ||
		strings.HasPrefix(name, ".") {
		return fmt.Errorf("%q is not a file name", name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("the name contains a control character")
		}
	}
	return nil
}
