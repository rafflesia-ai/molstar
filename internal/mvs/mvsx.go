package mvs

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Asset struct {
	Name string
	Path string
}

func WriteMVSX(path string, document Document, assets []Asset) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := MVSX(document, assets)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func MVSX(document Document, assets []Asset) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	data, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	if err := writeMVSXEntry(writer, "index.mvsj", data); err != nil {
		return nil, err
	}
	seen := map[string]bool{"index.mvsj": true}
	for _, asset := range assets {
		name, err := NormalizeAssetName(asset.Name)
		if err != nil {
			return nil, err
		}
		if name == "" || seen[name] {
			continue
		}
		content, err := os.ReadFile(asset.Path)
		if err != nil {
			return nil, err
		}
		if err := writeMVSXEntry(writer, name, content); err != nil {
			return nil, err
		}
		seen[name] = true
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// writeMVSXEntry adds a deflate-compressed zip entry WITHOUT a streaming data
// descriptor. Go's zip.Writer.Create emits data descriptors (local headers with
// zero CRC/size, values trailing after the data), which Mol*'s .mvsx unzip
// reader cannot parse — it reads the compressed size and CRC from the local
// header. CreateRaw with a fully-populated header writes those up front, so a
// Mol*-produced archive round-trips back through Mol* rendering.
func writeMVSXEntry(writer *zip.Writer, name string, content []byte) error {
	var compressed bytes.Buffer
	flateWriter, err := flate.NewWriter(&compressed, flate.DefaultCompression)
	if err != nil {
		return err
	}
	if _, err := flateWriter.Write(content); err != nil {
		return err
	}
	if err := flateWriter.Close(); err != nil {
		return err
	}
	header := &zip.FileHeader{
		Name:               name,
		Method:             zip.Deflate,
		CRC32:              crc32.ChecksumIEEE(content),
		CompressedSize64:   uint64(compressed.Len()),
		UncompressedSize64: uint64(len(content)),
	}
	entry, err := writer.CreateRaw(header)
	if err != nil {
		return err
	}
	_, err = entry.Write(compressed.Bytes())
	return err
}

func NormalizeAssetName(name string) (string, error) {
	normalized := filepath.ToSlash(strings.TrimPrefix(name, "/"))
	if normalized == "" {
		return "", fmt.Errorf("asset name is required")
	}
	if strings.Contains(normalized, "..") || filepath.IsAbs(normalized) {
		return "", fmt.Errorf("invalid asset name %q", name)
	}
	return normalized, nil
}

func ValidateMVSX(path string, maxBytes int64) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()

	var total uint64
	var hasIndex bool
	for _, file := range reader.File {
		name, err := NormalizeAssetName(file.Name)
		if err != nil {
			return err
		}
		total += file.UncompressedSize64
		if maxBytes > 0 && total > uint64(maxBytes) {
			return fmt.Errorf("mvsx archive exceeds runtime.max_archive_bytes=%d", maxBytes)
		}
		if name == "index.mvsj" {
			hasIndex = true
			opened, err := file.Open()
			if err != nil {
				return err
			}
			// Cap the actual decompressed read rather than trusting the ZIP
			// header's declared UncompressedSize64, so a lying header cannot
			// turn this into a decompression-bomb allocation.
			var source io.Reader = opened
			if maxBytes > 0 {
				source = io.LimitReader(opened, maxBytes+1)
			}
			data, err := io.ReadAll(source)
			_ = opened.Close()
			if err != nil {
				return err
			}
			if maxBytes > 0 && int64(len(data)) > maxBytes {
				return fmt.Errorf("mvsx index.mvsj exceeds runtime.max_archive_bytes=%d", maxBytes)
			}
			if _, err := Decode(data); err != nil {
				return fmt.Errorf("index.mvsj: %w", err)
			}
		}
	}
	if !hasIndex {
		return fmt.Errorf("mvsx archive is missing index.mvsj")
	}
	return nil
}

func ReplaceDownloadURLs(document Document, replacements map[string]string) Document {
	document = cloneDocument(document)
	var visit func(*Node)
	visit = func(node *Node) {
		if node.Kind == "download" && node.Params != nil {
			if raw, ok := node.Params["url"].(string); ok {
				if replacement, ok := replacements[raw]; ok {
					node.Params["url"] = replacement
				}
			}
		}
		for i := range node.Children {
			visit(&node.Children[i])
		}
	}
	visit(&document.Root)
	return document
}

func cloneDocument(document Document) Document {
	data, err := json.Marshal(document)
	if err != nil {
		return document
	}
	var cloned Document
	if err := json.Unmarshal(data, &cloned); err != nil {
		return document
	}
	return cloned
}
