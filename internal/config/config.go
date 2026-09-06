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

	// WebDir es el directorio con la interfaz ya compilada. Vacio en
	// desarrollo, donde la sirve Vite en otro puerto.
	WebDir string

	// PublicBaseURL es el origen desde el que los canales descargan las
	// imágenes de producto. En desarrollo apunta al localhost; en producción
	// debe ser una URL pública o los canales publicarán fichas sin fotos.
	PublicBaseURL string

	// ConciliacionCada es cada cuánto se vuelve a preguntar al canal si una
	// publicación sigue viva. Un día equilibra enterarse pronto de una baja
	// con no gastar en el informe el cupo de API que necesitan los envíos de
	// stock, que son los urgentes.
	ConciliacionCada time.Duration

	// RecrearPublicacionesCaidas decide qué hacer con una publicación que
	// desapareció del canal: volver a crearla sola —lo que viene puesto,
	// porque si no el producto se queda fuera de la venta hasta que alguien lo
	// note— o solo marcarla y avisar para que lo decida una persona.
	RecrearPublicacionesCaidas bool
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
		WebDir:               env("INTEGRA_WEB_DIR", ""),
		PublicBaseURL:        strings.TrimSuffix(env("INTEGRA_PUBLIC_BASE_URL", "http://localhost:8080"), "/"),

		ConciliacionCada:           envDuration("INTEGRA_CONCILIACION_CADA", 24*time.Hour),
		RecrearPublicacionesCaidas: envBool("INTEGRA_RECREAR_PUBLICACIONES_CAIDAS", true),
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
	// Con un periodo demasiado corto, la conciliación consulta el catálogo
	// entero una y otra vez y se lleva por delante el cupo de API que
	// necesitan los envíos de precio y stock.
	if c.ConciliacionCada < time.Minute {
		fallos = append(fallos, "INTEGRA_CONCILIACION_CADA debe ser de al menos 1m")
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

// envBool acepta las formas que la gente escribe de verdad en un .env. Un
// valor que no se entiende deja el valor por defecto: apagar sin querer una
// salvaguarda por una errata es peor que ignorar la línea.
func envBool(clave string, porDefecto bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(clave))) {
	case "1", "true", "si", "sí", "yes":
		return true
	case "0", "false", "no":
		return false
	default:
		return porDefecto
	}
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
