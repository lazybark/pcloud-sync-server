package config

import "github.com/spf13/viper"

type Config struct {
	ServerToken              string `mapstructure:"SERVER_TOKEN"`
	CertPath                 string `mapstructure:"CERT_PATH"`
	KeyPath                  string `mapstructure:"KEY_PATH"`
	HostName                 string `mapstructure:"HOST_NAME"`
	Port                     string `mapstructure:"PORT"`
	ServerVerboseLogging     bool   `mapstructure:"SERVER_VERBOSE_LOGGING"`
	CountStats               bool   `mapstructure:"COUNT_STATS"`
	FilesystemVerboseLogging bool   `mapstructure:"FILESYSTEM_VERBOSE_LOGGING"`
	SilentMode               bool   `mapstructure:"SILENT_MODE"`
	LogDirMain               string `mapstructure:"LOG_DIR_MAIN"`
	FileSystemRootPath       string `mapstructure:"FILE_SYSTEM_ROOT_PATH"`
	SQLiteDBName             string `mapstructure:"SQLITE_DB_NAME"`
}

var (
	Current Config
)

// LoadConfig reads configuration from file or environment variables.
func LoadConfig(path string) (err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("json")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&Current)
	if err != nil {
		return
	}

	return
}
