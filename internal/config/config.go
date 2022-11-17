// Copyright 2021-2022 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"go.uber.org/config"
)

var ErrConfigNotLoaded = errors.New("[ERR-289] config not loaded")

var conf = &Wrapper{}

type Section interface {
	Key() string
}

type Defaulter interface {
	SetDefaults()
}

type Validator interface {
	Validate() error
}

// Load loads the config file at the given path.
func Load(confFile string, overrides map[string]any) error {
	finfo, err := os.Stat(confFile)
	if err != nil {
		return fmt.Errorf("[ERR-290] failed to stat %s: %w", confFile, err)
	}

	if finfo.IsDir() {
		return fmt.Errorf("[ERR-291] config file path is a directory: %s", confFile)
	}

	return doLoad(config.File(confFile), config.Static(overrides))
}

func LoadReader(reader io.Reader, overrides map[string]any) error {
	return doLoad(config.Source(reader), config.Static(overrides))
}

func LoadMap(m map[string]any) error {
	return doLoad(config.Static(m))
}

func doLoad(sources ...config.YAMLOption) error {
	provider, err := mkProvider(sources...)
	if err != nil {
		return err
	}

	conf.replaceProvider(provider)
	return nil
}

func mkProvider(sources ...config.YAMLOption) (config.Provider, error) {
	opts := append(sources, config.Expand(os.LookupEnv)) //nolint:gocritic
	provider, err := config.NewYAML(opts...)
	if err != nil {
		if strings.Contains(err.Error(), "couldn't expand environment") {
			return nil, fmt.Errorf("[ERR-292] error loading configuration due to unknown environment variable. Config values containing '$' are interpreted as environment variables. Use '$$' to escape literal '$' values: [%w]", err)
		}
		return nil, fmt.Errorf("[ERR-293] failed to load config: %w", err)
	}

	return provider, err
}

// Global returns the default global config wrapper.
func Global() *Wrapper {
	return conf
}

// Get populates out with the configuration at the given key.
// Populate out with default values before calling this function to ensure sane defaults if there are any.
func Get(key string, out any) error {
	return conf.Get(key, out)
}

// GetSection populates a config section.
func GetSection(section Section) error {
	return conf.GetSection(section)
}

func WrapperFromReader(reader io.Reader, overrides map[string]any) (*Wrapper, error) {
	return newWrapper(config.Source(reader), config.Static(overrides))
}

func WrapperFromMap(m map[string]any) (*Wrapper, error) {
	return newWrapper(config.Static(m))
}

func newWrapper(sources ...config.YAMLOption) (*Wrapper, error) {
	provider, err := mkProvider(sources...)
	if err != nil {
		return nil, err
	}

	return &Wrapper{provider: provider}, nil
}

type Wrapper struct {
	provider config.Provider
	mu       sync.RWMutex
}

func (w *Wrapper) Get(key string, out any) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.provider == nil {
		if d, ok := out.(Defaulter); ok {
			d.SetDefaults()
			return nil
		}

		return ErrConfigNotLoaded
	}

	// set defaults if any are specified
	if d, ok := out.(Defaulter); ok {
		d.SetDefaults()
	}

	if err := w.provider.Get(key).Populate(out); err != nil {
		return err
	}

	// validate if a validate function is available
	if v, ok := out.(Validator); ok {
		return v.Validate()
	}

	return nil
}

func (w *Wrapper) GetSection(section Section) error {
	return w.Get(section.Key(), section)
}

func (w *Wrapper) replaceProvider(provider config.Provider) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.provider = provider
}
