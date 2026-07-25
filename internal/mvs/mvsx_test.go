package mvs

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestMVSXIncludesIndexAndAssets(t *testing.T) {
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "model.cif")
	if err := os.WriteFile(assetPath, []byte("data_test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	document := Document{
		Metadata: Metadata{Version: "1"},
		Root: Node{
			Kind: "root",
			Children: []Node{{
				Kind: "download",
				Params: map[string]any{
					"url": "assets/model.cif",
				},
			}},
		},
	}
	data, err := MVSX(document, []Asset{{Name: "assets/model.cif", Path: assetPath}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(opened)
		_ = opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(content)
	}
	if files["index.mvsj"] == "" {
		t.Fatalf("expected index.mvsj in archive, got %#v", files)
	}
	if files["assets/model.cif"] != "data_test\n" {
		t.Fatalf("expected asset contents, got %#v", files["assets/model.cif"])
	}
}

// TestMVSXHasNoDataDescriptors locks in that .mvsx entries are written without a
// streaming data descriptor (general-purpose flag bit 3). Mol*'s .mvsx unzip
// reader reads the compressed size/CRC from the local file header, so an archive
// written with data descriptors fails to render — the entries must be readable
// with the sizes present up front.
func TestMVSXHasNoDataDescriptors(t *testing.T) {
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "model.cif")
	content := append([]byte("data_test\n"), bytes.Repeat([]byte("ATOM  "), 500)...)
	if err := os.WriteFile(assetPath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	document := Document{Metadata: Metadata{Version: "1"}, Root: Node{Kind: "root"}}
	data, err := MVSX(document, []Asset{{Name: "assets/model.cif", Path: assetPath}})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) < 2 {
		t.Fatalf("expected index + asset entries, got %d", len(reader.File))
	}
	for _, file := range reader.File {
		if file.Flags&0x8 != 0 {
			t.Errorf("entry %q was written with a data descriptor (flags=0x%04x); Mol* cannot read it", file.Name, file.Flags)
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open %q: %v", file.Name, err)
		}
		if _, err := io.ReadAll(rc); err != nil {
			t.Fatalf("read %q: %v", file.Name, err)
		}
		_ = rc.Close()
	}
}

func TestReplaceDownloadURLs(t *testing.T) {
	document := Document{
		Metadata: Metadata{Version: "1"},
		Root: Node{
			Kind: "root",
			Children: []Node{{
				Kind: "download",
				Params: map[string]any{
					"url": "file:///tmp/model.cif",
				},
			}},
		},
	}
	replaced := ReplaceDownloadURLs(document, map[string]string{"file:///tmp/model.cif": "assets/model.cif"})
	if replaced.Root.Children[0].Params["url"] != "assets/model.cif" {
		t.Fatalf("download URL was not replaced: %#v", replaced.Root.Children[0].Params)
	}
	if document.Root.Children[0].Params["url"] != "file:///tmp/model.cif" {
		t.Fatalf("source document was mutated: %#v", document.Root.Children[0].Params)
	}
}
