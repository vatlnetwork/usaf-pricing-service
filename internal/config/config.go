package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Server  Server  `json:"server"`
	MongoDB MongoDB `json:"mongodb"`
}

type Server struct {
	Address string `json:"address"`
}

type MongoDB struct {
	URI                     string `json:"uri"`
	Database                string `json:"database"`
	Collection              string `json:"collection"`
	ConnectTimeoutSeconds   int    `json:"connect_timeout_seconds"`
	OperationTimeoutSeconds int    `json:"operation_timeout_seconds"`
}

func (m MongoDB) ConnectTimeout() time.Duration {
	return time.Duration(m.ConnectTimeoutSeconds) * time.Second
}

func (m MongoDB) OperationTimeout() time.Duration {
	return time.Duration(m.OperationTimeoutSeconds) * time.Second
}

func Load(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()
	cfg := &Config{
		Server: Server{Address: ":8080"},
		MongoDB: MongoDB{
			URI: "mongodb://localhost:27017", Database: "usaf_pricing", Collection: "vendor_programs",
			ConnectTimeoutSeconds: 10, OperationTimeoutSeconds: 5,
		},
	}
	decoder := json.NewDecoder(f)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Config{}, errors.New("config must contain a single JSON object")
	}
	if cfg == nil {
		return Config{}, errors.New("config must be a JSON object")
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return *cfg, nil
}

func (c Config) validate() error {
	_, port, err := net.SplitHostPort(c.Server.Address)
	if err != nil {
		return errors.New("server.address must be host:port (for example :8080)")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("server.address must use a port between 1 and 65535")
	}
	if !strings.HasPrefix(c.MongoDB.URI, "mongodb://") && !strings.HasPrefix(c.MongoDB.URI, "mongodb+srv://") {
		return errors.New("mongodb.uri must start with mongodb:// or mongodb+srv://")
	}
	if strings.TrimSpace(c.MongoDB.Database) == "" || strings.TrimSpace(c.MongoDB.Collection) == "" {
		return errors.New("mongodb.database and mongodb.collection are required")
	}
	if c.MongoDB.ConnectTimeoutSeconds < 1 || c.MongoDB.ConnectTimeoutSeconds > 300 ||
		c.MongoDB.OperationTimeoutSeconds < 1 || c.MongoDB.OperationTimeoutSeconds > 300 {
		return errors.New("mongodb timeout values must be between 1 and 300 seconds")
	}
	return nil
}
