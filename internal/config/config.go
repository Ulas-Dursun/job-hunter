package config

import (
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// Config, uygulamanın tüm konfigürasyonunu tutar.
// config.yaml'dan bir kez yüklenir, startup'ta tüm servislere pointer olarak geçilir.
type Config struct {
	Candidate     CandidateConfig     `yaml:"candidate"`
	Scoring       ScoringConfig       `yaml:"scoring"`
	Sources       SourcesConfig       `yaml:"sources"`
	Redis         RedisConfig         `yaml:"redis"`
	Server        ServerConfig        `yaml:"server"`
	Gemini        GeminiConfig        `yaml:"gemini"`
	Worker        WorkerConfig        `yaml:"worker"`
	Notifications NotificationsConfig `yaml:"notifications"` // YENİ
}
type NotificationsConfig struct {
	TelegramEnabled bool `yaml:"telegram_enabled"`
}
type CandidateConfig struct {
	Name              string          `yaml:"name"`
	Location          string          `yaml:"location"`
	Status            string          `yaml:"status"`
	GraduationYear    int             `yaml:"graduation_year"`
	Education         string          `yaml:"education"`
	MobilityNote      string          `yaml:"mobility_note"`
	SpokenLanguages   string          `yaml:"spoken_languages"`
	Links             string          `yaml:"links"`
	SummaryVariants   []string        `yaml:"summary_variants"`
	Experience        []string        `yaml:"experience"`
	Projects          []string        `yaml:"projects"`
	Skills            SkillsConfig    `yaml:"skills"`
	TargetRoles       TargetRoles     `yaml:"target_roles"`
	Seniority         SeniorityConfig `yaml:"seniority"`
	WorkPreference    []string        `yaml:"work_preference"`
	BlacklistKeywords []string        `yaml:"blacklist_keywords"`
	PriorityCompanies []string        `yaml:"priority_companies"`
}

type SkillsConfig struct {
	Languages  []string `yaml:"languages"`
	Frameworks []string `yaml:"frameworks"`
	Databases  []string `yaml:"databases"`
	OS         []string `yaml:"os"`
	Infra      []string `yaml:"infra"`
	Cloud      []string `yaml:"cloud"`
	Concepts   []string `yaml:"concepts"`
}

type TargetRoles struct {
	Primary   []string `yaml:"primary"`
	Secondary []string `yaml:"secondary"`
}

type SeniorityConfig struct {
	Preferred  []string `yaml:"preferred"`
	Acceptable []string `yaml:"acceptable"`
	Rejected   []string `yaml:"rejected"`
}

type ScoringConfig struct {
	TelegramThresholdPrimary   float64 `yaml:"telegram_threshold_primary"`
	TelegramThresholdSecondary float64 `yaml:"telegram_threshold_secondary"`
	SaveThreshold              float64 `yaml:"save_threshold"`
}

type SourcesConfig struct {
	JobSpy      SourceConfig `yaml:"jobspy"`
	ATS         SourceConfig `yaml:"ats"`
	Eurocontrol SourceConfig `yaml:"eurocontrol"`
	Techcareer  SourceConfig `yaml:"techcareer"`
	Coderspace  SourceConfig `yaml:"coderspace"`
	Arbeitnow   SourceConfig `yaml:"arbeitnow"`
}

type SourceConfig struct {
	Enabled             bool     `yaml:"enabled"`
	PollIntervalMinutes int      `yaml:"poll_interval_minutes"`
	MaxAgeDays          int      `yaml:"max_age_days"`
	Keywords            []string `yaml:"keywords"`
	Location            string   `yaml:"location"`
}

type RedisConfig struct {
	StreamRaw      string `yaml:"stream_raw"`
	StreamFiltered string `yaml:"stream_filtered"`
	StreamFailed   string `yaml:"stream_failed"`
	ConsumerGroup  string `yaml:"consumer_group"`
	ConsumerName   string `yaml:"consumer_name"`
	DedupSetKey    string `yaml:"dedup_set_key"`
	DedupTTLDays   int    `yaml:"dedup_ttl_days"`
}

type ServerConfig struct {
	MetricsPort   int `yaml:"metrics_port"`
	DashboardPort int `yaml:"dashboard_port"` // YENİ
}

// Load, config.yaml dosyasını (varsa yerel config.local.yaml override dosyasını) okuyup parse eder ve doğrular.
// Sadece startup'ta bir kez çağrılır.
func Load(path string) (*Config, error) {
	if path == "config.yaml" {
		if _, err := os.Stat("config.local.yaml"); err == nil {
			path = "config.local.yaml"
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config yaml: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &cfg, nil
}

// validate, zorunlu alanların dolu olduğunu kontrol eder.
// Hatalı config ile sistem başlatılmaz — fail fast prensibi.
func (c *Config) validate() error {
	if c.Candidate.Name == "" {
		return fmt.Errorf("candidate.name is required")
	}
	if len(c.Candidate.TargetRoles.Primary) == 0 {
		return fmt.Errorf("candidate.target_roles.primary must have at least one entry")
	}
	if c.Redis.StreamRaw == "" {
		return fmt.Errorf("redis.stream_raw is required")
	}
	if c.Scoring.TelegramThresholdPrimary == 0 {
		return fmt.Errorf("scoring.telegram_threshold_primary is required")
	}
	if c.Server.MetricsPort == 0 {
		return fmt.Errorf("server.metrics_port is required")
	}
	return nil
}

// PostgresDSN, environment variable'lardan PostgreSQL bağlantı string'i üretir.
// Şifreler YAML'da değil, env'de tutulur.
func PostgresDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		getEnv("POSTGRES_USER", "jobhunter"),
		getEnv("POSTGRES_PASSWORD", "jobhunter"),
		getEnv("POSTGRES_HOST", "localhost"),
		getEnv("POSTGRES_PORT", "5432"),
		getEnv("POSTGRES_DB", "jobhunter"),
	)
}

// RedisAddr, environment variable'lardan Redis adresini üretir.
func RedisAddr() string {
	return fmt.Sprintf("%s:%s",
		getEnv("REDIS_HOST", "localhost"),
		getEnv("REDIS_PORT", "6379"),
	)
}

// MaskedPostgresDSN, şifreyi gizleyerek DSN'i loglamak için kullanılır.
func MaskedPostgresDSN() string {
	u, err := url.Parse(PostgresDSN())
	if err != nil {
		return "[unparseable dsn]"
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.String()
}

func getEnv(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// Dosyanın sonuna ekle:

type GeminiConfig struct {
	Model        string `yaml:"model"`
	MaxTokens    int    `yaml:"max_tokens"`
	RateLimitRPM int    `yaml:"rate_limit_rpm"`
}

type WorkerConfig struct {
	PoolSize int `yaml:"pool_size"`
}
