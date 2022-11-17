// Copyright 2021-2022 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"
)

var supportedFileTypes = map[string]struct{}{".yaml": {}, ".yml": {}, ".json": {}}

var ErrNoMatchingFiles = errors.New("[ERR-617] no matching files")

// SchemasDirectory is the name of the special directory containing schemas. It is defined here to avoid an import loop.
const SchemasDirectory = "_schemas"

// TestDataDirectory is the name of the special directory containing test fixtures. It is defined here to avoid an import loop.
const TestDataDirectory = "testdata"

// IsSupportedTestFile return true if the given file is a supported test file name, i.e. "*_test.{yaml,yml,json}".
func IsSupportedTestFile(fileName string) bool {
	if ext, ok := IsSupportedFileTypeExt(fileName); ok {
		f := strings.ToLower(fileName)
		return strings.HasSuffix(f[:len(f)-len(ext)], "_test")
	}
	return false
}

// IsSupportedFileTypeExt returns true and a file extension if the given file has a supported file extension.
func IsSupportedFileTypeExt(fileName string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(fileName))
	_, exists := supportedFileTypes[ext]

	return ext, exists
}

// IsJSONFileTypeExt returns true if the given file has a json file extension.
func IsJSONFileTypeExt(fileName string) bool {
	ext := strings.ToLower(filepath.Ext(fileName))
	return ext == ".json"
}

// IsSupportedFileType returns true if the given file has a supported file extension.
func IsSupportedFileType(fileName string) bool {
	_, ok := IsSupportedFileTypeExt(fileName)
	return ok
}

func IsHidden(fileName string) bool {
	switch fileName {
	case ".", "..":
		return false
	default:
		return strings.HasPrefix(fileName, ".")
	}
}

// LoadFromJSONOrYAML reads a JSON or YAML encoded protobuf from the given path.
func LoadFromJSONOrYAML(fsys fs.FS, path string, dest proto.Message) error {
	f, err := fsys.Open(path)
	if err != nil {
		return fmt.Errorf("[ERR-618] failed to open %s: %w", path, err)
	}

	defer f.Close()

	return ReadJSONOrYAML(f, dest)
}

// OpenOneOfSupportedFiles attempts to open a fileName adding supported extensions.
func OpenOneOfSupportedFiles(fsys fs.FS, fileName string) (fs.File, error) {
	matches, err := fs.Glob(fsys, fileName+".*")
	if err != nil {
		return nil, err
	}
	var filepath string
	for _, match := range matches {
		if IsSupportedFileType(match) {
			filepath = match
			break
		}
	}
	if filepath == "" {
		return nil, ErrNoMatchingFiles
	}

	file, err := fsys.Open(filepath)
	if err != nil {
		return nil, err
	}

	return file, nil
}

type IndexedFileType uint8

const (
	FileTypeNotIndexed IndexedFileType = iota
	FileTypePolicy
	FileTypeSchema
)

// FileType categorizes the given path according to how it will be treated by the index.
// The path must be "/"-separated and relative to the root policies directory.
func FileType(path string) IndexedFileType {
	segments := strings.Split(path, "/")
	fileName := segments[len(segments)-1]

	inSchemas := segments[0] == SchemasDirectory

	for _, segment := range segments {
		if IsHidden(segment) || (segment == TestDataDirectory && !inSchemas) {
			return FileTypeNotIndexed
		}
	}

	if inSchemas {
		if IsJSONFileTypeExt(fileName) {
			return FileTypeSchema
		}

		return FileTypeNotIndexed
	}

	if IsSupportedFileType(fileName) && !IsSupportedTestFile(fileName) {
		return FileTypePolicy
	}

	return FileTypeNotIndexed
}

// RelativeSchemaPath returns the given path within the top-level schemas directory,
// and a flag to indicate whether the path was actually contained in that directory.
// The path must be "/"-separated and relative to the root policies directory.
func RelativeSchemaPath(path string) (string, bool) {
	schemaPath := strings.TrimPrefix(path, SchemasDirectory+"/")
	if schemaPath == path {
		return "", false
	}

	return schemaPath, true
}
