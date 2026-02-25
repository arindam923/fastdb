package main

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"k8s.io/klog/v2"

	"github.com/alecthomas/kingpin/v2"
)

type Config struct {
	Port            string `yaml:"port"`
	Host            string `yaml:"host"`
	DataDir         string `yaml:"data-dir"`
	JWTSecret       string `yaml:"jwt-secret"`
	PersistInterval string `yaml:"persist-interval"`
	LogLevel        string `yaml:"log-level"`
}

var (
	port            = kingpin.Flag("port", "Server port").Short('p').Default("8080").Envar("PORT").String()
	host            = kingpin.Flag("host", "Bind address").Short('h').Default("0.0.0.0").Envar("HOST").String()
	dataDir         = kingpin.Flag("data-dir", "Data directory").Short('d').Default("./flashdb-data").Envar("DATA_DIR").String()
	cfgJWTFlag      = kingpin.Flag("jwt-secret", "JWT signing secret (leave empty to auto-generate)").Short('j').Envar("JWT_SECRET").String()
	configFile      = kingpin.Flag("config", "Config file path").Short('c').ExistingFile()
	persistInterval = kingpin.Flag("persist-interval", "How often to persist data to disk").Default("5s").Envar("PERSIST_INTERVAL").String()
	logLevel        = kingpin.Flag("log-level", "Log level (debug, info, warn, error)").Default("info").Envar("LOG_LEVEL").String()
)

func ParseAndValidate() *Config {
	kingpin.Parse()
	return loadConfig()
}

func loadConfig() *Config {
	cfg := &Config{
		Port:            *port,
		Host:            *host,
		DataDir:         *dataDir,
		JWTSecret:       *cfgJWTFlag,
		PersistInterval: *persistInterval,
		LogLevel:        *logLevel,
	}

	if *configFile != "" {
		data, err := os.ReadFile(*configFile)
		if err != nil {
			klog.Exitf("Failed to read config file: %v", err)
		}
		var fileCfg Config
		if err := yaml.Unmarshal(data, &fileCfg); err != nil {
			klog.Exitf("Failed to parse config file: %v", err)
		}
		if fileCfg.Port != "" && cfg.Port == "8080" {
			cfg.Port = fileCfg.Port
		}
		if fileCfg.Host != "" && cfg.Host == "0.0.0.0" {
			cfg.Host = fileCfg.Host
		}
		if fileCfg.DataDir != "" {
			cfg.DataDir = fileCfg.DataDir
		}
		if fileCfg.JWTSecret != "" {
			cfg.JWTSecret = fileCfg.JWTSecret
		}
		if fileCfg.PersistInterval != "" {
			cfg.PersistInterval = fileCfg.PersistInterval
		}
		if fileCfg.LogLevel != "" {
			cfg.LogLevel = fileCfg.LogLevel
		}
	}

	return cfg
}

func ensureConfig(cfg *Config) error {
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(cfg.DataDir, "flashdb.yaml")

	_, err := os.Stat(configPath)
	if err == nil {
		data, err := os.ReadFile(configPath)
		if err == nil {
			var fileCfg Config
			if yaml.Unmarshal(data, &fileCfg) == nil {
				if cfg.JWTSecret == "" && fileCfg.JWTSecret != "" {
					cfg.JWTSecret = fileCfg.JWTSecret
				}
			}
		}
		return nil
	}

	if cfg.JWTSecret == "" {
		cfg.JWTSecret = generateJWTSecret()
		klog.Infof("🔐 Generated new JWT secret")
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return err
	}

	klog.Infof("📁 Created config file at %s", configPath)
	return nil
}

func generateJWTSecret() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		klog.Errorf("Failed to generate random JWT secret: %v", err)
		return ""
	}
	return hex.EncodeToString(bytes)
}
