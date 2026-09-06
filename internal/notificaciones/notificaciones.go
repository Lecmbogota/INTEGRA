// Package notificaciones saca las alertas de la base y las manda a donde
// alguien las vea.
//
// Sin esto, el módulo de alertas es un panel que nadie mira: un token vencido
// o un pedido sin mapear se quedan esperando a que a alguien se le ocurra
// entrar. El canal elegido es el correo, por ser el que no depende de ningún
// servicio externo ni de que el destinatario instale nada.
package notificaciones

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/store"
)

// ConfigSMTP es lo que se guarda cifrado en notification_destinations.
type ConfigSMTP struct {
	Host          string   `json:"host"`
	Puerto        int      `json:"puerto"`
	Usuario       string   `json:"usuario"`
	Password      string   `json:"password"`
	Remitente     string   `json:"remitente"`
	Destinatarios []string `json:"destinatarios"`
	// SinTLS permite un relay interno sin cifrado. Por defecto se exige TLS:
	// mandar la contraseña en claro por la red no puede ser lo que pase si
	// alguien deja un campo vacío.
	SinTLS bool `json:"sin_tls,omitempty"`
}

func (c ConfigSMTP) valida() error {
	if c.Host == "" || c.Remitente == "" || len(c.Destinatarios) == 0 {
		return fmt.Errorf("faltan el servidor, el remitente o los destinatarios")
	}
	return nil
}

func (c ConfigSMTP) direccion() string {
	p := c.Puerto
	if p == 0 {
		p = 587
	}
	return net.JoinHostPort(c.Host, fmt.Sprint(p))
}

// almacen es lo que este paquete necesita de la persistencia. Se declara aquí
// para poder probar el envío sin base de datos.
type almacen interface {
	DestinosActivos(ctx context.Context) ([]store.DestinoNotificacion, error)
	AlertasSinNotificar(ctx context.Context, minSeveridad string, limite int) ([]store.AlertaPendiente, error)
	MarcarAlertasNotificadas(ctx context.Context, ids []int64) error
	AnotarEnvioNotificacion(ctx context.Context, destinoID int64, err error) error
}

// Enviador manda un correo ya compuesto. Se inyecta para que las pruebas no
// necesiten un servidor SMTP.
type Enviador func(cfg ConfigSMTP, asunto, cuerpo string) error

type Servicio struct {
	st  almacen
	cif *crypto.Cifrador
	log *slog.Logger
	env Enviador
}

func Nuevo(st *store.Store, cif *crypto.Cifrador, log *slog.Logger) *Servicio {
	return nuevoCon(st, cif, log, EnviarSMTP)
}

func nuevoCon(st almacen, cif *crypto.Cifrador, log *slog.Logger, env Enviador) *Servicio {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Servicio{st: st, cif: cif, log: log, env: env}
}

// Despachar manda las alertas pendientes a cada destino activo.
//
// Se llama desde el planificador en cada pasada. Es tolerante a fallos por
// diseño: que un destino esté mal configurado no puede impedir que los demás
// reciban, ni que la pasada del planificador termine.
func (s *Servicio) Despachar(ctx context.Context) error {
	destinos, err := s.st.DestinosActivos(ctx)
	if err != nil {
		return err
	}
	if len(destinos) == 0 {
		return nil // nadie configuró a dónde mandar: no es un error
	}

	for _, d := range destinos {
		if d.Tipo != "email" {
			// Telegram y webhook están en el esquema y no se implementan aún:
			// mejor no tocarlos que fingir que salieron.
			continue
		}
		if err := s.despacharCorreo(ctx, d); err != nil {
			s.log.Error("no se pudieron enviar las alertas", "destino", d.Nombre, "error", err)
			_ = s.st.AnotarEnvioNotificacion(ctx, d.ID, err)
		}
	}
	return nil
}

func (s *Servicio) despacharCorreo(ctx context.Context, d store.DestinoNotificacion) error {
	claro, err := s.cif.DescifrarTexto(d.ConfigEnc)
	if err != nil {
		return fmt.Errorf("no se pudo descifrar la configuración: %w", err)
	}
	var cfg ConfigSMTP
	if err := json.Unmarshal([]byte(claro), &cfg); err != nil {
		return fmt.Errorf("configuración ilegible: %w", err)
	}
	if err := cfg.valida(); err != nil {
		return err
	}

	pendientes, err := s.st.AlertasSinNotificar(ctx, d.MinSeveridad, 20)
	if err != nil {
		return err
	}
	if len(pendientes) == 0 {
		return nil
	}

	asunto, cuerpo := componer(pendientes)
	if err := s.env(cfg, asunto, cuerpo); err != nil {
		return err
	}

	// Solo se sellan tras el envío: hacerlo antes convertiría un fallo del
	// servidor de correo en alertas que nadie ve jamás.
	ids := make([]int64, 0, len(pendientes))
	for _, a := range pendientes {
		ids = append(ids, a.ID)
	}
	if err := s.st.MarcarAlertasNotificadas(ctx, ids); err != nil {
		return err
	}
	_ = s.st.AnotarEnvioNotificacion(ctx, d.ID, nil)
	s.log.Info("alertas notificadas por correo", "destino", d.Nombre, "alertas", len(ids))
	return nil
}

// componer arma un solo correo con todas las alertas pendientes.
//
// Uno por alerta llenaría la bandeja en un incidente y entrenaría a la gente
// a ignorarlos, que es justo lo contrario de lo que se busca.
func componer(as []store.AlertaPendiente) (asunto, cuerpo string) {
	peor := "info"
	for _, a := range as {
		if pesoSeveridad(a.Severidad) > pesoSeveridad(peor) {
			peor = a.Severidad
		}
	}
	if len(as) == 1 {
		asunto = fmt.Sprintf("Integra: %s", as[0].Mensaje)
	} else {
		asunto = fmt.Sprintf("Integra: %d avisos (%s)", len(as), peor)
	}

	var b strings.Builder
	b.WriteString("Avisos abiertos en Integra:\n\n")
	for _, a := range as {
		fmt.Fprintf(&b, "  [%s] %s\n", a.Severidad, a.Mensaje)
	}
	b.WriteString("\nEstán en la sección Automatización de la interfaz, donde se pueden\n")
	b.WriteString("marcar como vistos. Este correo no se repite: cada aviso se envía una vez.\n")
	return asunto, b.String()
}

func pesoSeveridad(s string) int {
	switch s {
	case "critical":
		return 3
	case "error":
		return 2
	case "warning":
		return 1
	}
	return 0
}

// EnviarSMTP entrega el correo de verdad.
func EnviarSMTP(cfg ConfigSMTP, asunto, cuerpo string) error {
	mensaje := construirMensaje(cfg, asunto, cuerpo)

	var auth smtp.Auth
	if cfg.Usuario != "" {
		auth = smtp.PlainAuth("", cfg.Usuario, cfg.Password, cfg.Host)
	}
	if cfg.SinTLS {
		return smtp.SendMail(cfg.direccion(), auth, cfg.Remitente, cfg.Destinatarios, mensaje)
	}

	// smtp.SendMail ya hace STARTTLS cuando el servidor lo anuncia, pero no
	// falla si no lo anuncia: seguiría y mandaría la contraseña en claro. Aquí
	// se exige explícitamente.
	c, err := smtp.Dial(cfg.direccion())
	if err != nil {
		return fmt.Errorf("conectando con %s: %w", cfg.Host, err)
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("el servidor %s no ofrece STARTTLS; usa sin_tls solo si es un relay interno de confianza", cfg.Host)
	}
	if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
		return fmt.Errorf("negociando TLS: %w", err)
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("autenticando en %s: %w", cfg.Host, err)
		}
	}
	if err := c.Mail(cfg.Remitente); err != nil {
		return err
	}
	for _, d := range cfg.Destinatarios {
		if err := c.Rcpt(d); err != nil {
			return fmt.Errorf("destinatario %s: %w", d, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(mensaje); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func construirMensaje(cfg ConfigSMTP, asunto, cuerpo string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", cfg.Remitente)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(cfg.Destinatarios, ", "))
	// El asunto lleva acentos y nombres de producto: codificarlo evita que
	// llegue como caracteres rotos en cualquier cliente.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", asunto))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	// Un punto al principio de línea termina el mensaje en SMTP: hay que
	// duplicarlo o el correo se corta a la mitad.
	b.WriteString(strings.ReplaceAll(cuerpo, "\n.", "\n.."))
	return []byte(b.String())
}
