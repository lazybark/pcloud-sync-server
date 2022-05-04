package server

import "github.com/spf13/viper"

type (
	// Config defines server behaviour
	Config struct {
		ServerToken              string `mapstructure:"SERVER_TOKEN"`
		CertPath                 string `mapstructure:"CERT_PATH"`
		KeyPath                  string `mapstructure:"KEY_PATH"`
		HostName                 string `mapstructure:"HOST_NAME"`
		Port                     string `mapstructure:"PORT"`
		MaxClientErrors          uint   // Limit for client-side errors (or any other party) until problematic connection will be closed and ErrTooMuchClientErrors sent
		MaxServerErrors          uint   // Limit for server-side errors until problematic connection will be closed and ErrTooMuchServerErrors sent
		LogStats                 bool
		CollectStats             bool
		TokenValidDays           int
		ServerVerboseLogging     bool   `mapstructure:"SERVER_VERBOSE_LOGGING"`
		CountStats               bool   `mapstructure:"COUNT_STATS"`
		FilesystemVerboseLogging bool   `mapstructure:"FILESYSTEM_VERBOSE_LOGGING"`
		SilentMode               bool   `mapstructure:"SILENT_MODE"`
		LogDirMain               string `mapstructure:"LOG_DIR_MAIN"`
		FileSystemRootPath       string `mapstructure:"FILE_SYSTEM_ROOT_PATH"`
		SQLiteDBName             string `mapstructure:"SQLITE_DB_NAME"`
		ServerName               string `mapstructure:"SERVER_NAME"`
		MaxConnectionsPerUser    int    `mapstructure:"MAX_USER_CONNECTIONS_PER_USER"`
	}
)

// LoadConfig reads configuration from file or environment variables.
func (s *Server) LoadConfig(path string) (err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("json")

	viper.AutomaticEnv()

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	err = viper.Unmarshal(&s.config)
	if err != nil {
		return
	}

	return
}
