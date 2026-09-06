// Package config carga la configuración desde el entorno.
//
// Todo secreto llega por variable de entorno y nada se guarda en el
// repositorio. La carga falla en el arranque si falta algo obligatorio: es
// preferible no arrancar a descubrir a mitad de una sincronización que la
// clave maestra no estaba puesta.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL string
	MasterKey   string

	HTTPAddr        string
	LogLevel        string
	DefaultTimezone string

	// WorkerConcurrency limita cuántos trabajos corren a la vez. El límite
	// real de rendimiento son los cupos de API de los canales, no la CPU.
	WorkerConcurrency int

	// SchedulerTick es cada cuánto se revisa sync_schedules. Un minuto da una
	// precisión más que suficiente para horarios diarios.
	SchedulerTick time.Duration

	// APICallRetentionDays limita cuánto se guarda el detalle de peticiones y
	// respuestas: es la tabla que más crece con diferencia.
	APICallRetentionDays int

	// ImageDir es el directorio del banco de imágenes. Vacío lo desactiva y la
	// API responde 503 en esas rutas en vez de impedir el arranque.
	ImageDir string

	// PublicBaseURL es el origen desde el que los canales descargan las
	// imágenes de producto. En desarrollo apunta al localhost; en producción
	// debe ser una URL pública o los canales publicarán fichas sin fotos.
	PublicBaseURL string
}

// Load lee la configuración del entorno (cargando .env si existe) y la valida.
func Load() (*Config, error) {
	cargarDotEnv(".env")

	c := &Config{
		DatabaseURL:          env("INTEGRA_DATABASE_URL", ""),
		MasterKey:            env("INTEGRA_MASTER_KEY", ""),
		HTTPAddr:             env("INTEGRA_HTTP_ADDR", ":8080"),
		LogLevel:             env("INTEGRA_LOG_LEVEL", "info"),
		DefaultTimezone:      env("INTEGRA_TIMEZONE", "America/Bogota"),
		WorkerConcurrency:    envInt("INTEGRA_WORKER_CONCURRENCY", 8),
		SchedulerTick:        envDuration("INTEGRA_SCHEDULER_TICK", time.Minute),
		APICallRetentionDays: envInt("INTEGRA_API_CALL_RETENTION_DAYS", 30),
		ImageDir:             env("INTEGRA_IMAGE_DIR", "./datos/imagenes"),
		PublicBaseURL:        strings.TrimSuffix(env("INTEGRA_PUBLIC_BASE_URL", "http://localhost:8080"), "/"),
	}
	if err := c.validar(); err != nil {
		return nil, err
	}
	return c, nil
}

// cargarDotEnv lee pares CLAVE=VALOR de un archivo .env y los setea en el entorno si no existen.
func cargarDotEnv(ruta string) {
	datos, err := os.ReadFile(ruta)
	if err != nil {
		return
	}
	lineas := strings.Split(string(datos), "\n")
	for _, l := range lineas {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		partes := strings.SplitN(l, "=", 2)
		if len(partes) != 2 {
			continue
		}
		k := strings.TrimSpace(partes[0])
		v := strings.TrimSpace(partes[1])
		// Remover comillas si las tiene
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}

func (c *Config) validar() error {
	var fallos []string

	if c.DatabaseURL == "" {
		fallos = append(fallos, "INTEGRA_DATABASE_URL es obligatoria")
	}
	if c.MasterKey == "" {
		fallos = append(fallos,
			"INTEGRA_MASTER_KEY es obligatoria (genérala con: go run ./cmd/integra genkey)")
	}
	if _, err := time.LoadLocation(c.DefaultTimezone); err != nil {
		fallos = append(fallos, fmt.Sprintf("INTEGRA_TIMEZONE %q no es una zona válida", c.DefaultTimezone))
	}
	if c.WorkerConcurrency < 1 {
		fallos = append(fallos, "INTEGRA_WORKER_CONCURRENCY debe ser al menos 1")
	}
	if c.SchedulerTick < time.Second {
		fallos = append(fallos, "INTEGRA_SCHEDULER_TICK debe ser de al menos 1s")
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		fallos = append(fallos, fmt.Sprintf(
			"INTEGRA_LOG_LEVEL %q no es válido (debug, info, warn o error)", c.LogLevel))
	}

	if len(fallos) > 0 {
		return errors.New("configuración inválida:\n  - " + strings.Join(fallos, "\n  - "))
	}
	return nil
}

func env(clave, porDefecto string) string {
	if v := strings.TrimSpace(os.Getenv(clave)); v != "" {
		return v
	}
	return porDefecto
}

func envInt(clave string, porDefecto int) int {
	v := strings.TrimSpace(os.Getenv(clave))
	if v == "" {
		return porDefecto
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return porDefecto
	}
	return n
}

func envDuration(clave string, porDefecto time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(clave))
	if v == "" {
		return porDefecto
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return porDefecto
	}
	return d
}
