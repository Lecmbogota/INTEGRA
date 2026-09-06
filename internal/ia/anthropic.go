package ia

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
)

// Proveedor remoto sobre la API de Claude.
//
// Ya no es el camino por defecto, pero se queda: para un lote donde la
// redacción importe —una marca nueva, una categoría que se va a promocionar—
// la diferencia de calidad frente a un modelo de 3B es grande, y entonces sí
// compensa el coste. Se activa con IA_PROVEEDOR=anthropic.
//
// La credencial se resuelve como en cualquier herramienta de Anthropic:
// ANTHROPIC_API_KEY en el entorno.

// El contenido de las fichas es lo que ve el comprador: aquí la calidad paga
// más que el ahorro, y por eso este proveedor no baja de Opus.
const modeloAnthropic = "claude-opus-5"

type Anthropic struct{ cli anthropic.Client }

func NuevoAnthropic() *Anthropic {
	// El constructor sin argumentos resuelve la credencial del entorno.
	return &Anthropic{cli: anthropic.NewClient()}
}

func (a *Anthropic) Descripcion() string {
	return fmt.Sprintf("API de Claude (%s)", modeloAnthropic)
}

func (a *Anthropic) GenerarFicha(ctx context.Context, nombre, marca, categoria, sku, specsPrevias string) (*Ficha, error) {
	resp, err := a.cli.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     modeloAnthropic,
		MaxTokens: 2048,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(
				promptFicha(nombre, marca, categoria, sku, specsPrevias))),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generando ficha de %q: %w", sku, err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, fmt.Errorf("la generación fue rechazada por el modelo para %q", sku)
	}

	var f Ficha
	if err := json.Unmarshal([]byte(extraerJSON(textoDe(resp))), &f); err != nil {
		return nil, fmt.Errorf("respuesta ilegible para %q: %w", sku, err)
	}
	if err := validarFicha(&f, sku); err != nil {
		return nil, err
	}
	return &f, nil
}

func (a *Anthropic) VerificarImagen(ctx context.Context, jpeg []byte, nombre, marca, sku string) (*Veredicto, error) {
	resp, err := a.cli.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     modeloAnthropic,
		MaxTokens: 256,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewImageBlockBase64("image/jpeg", base64.StdEncoding.EncodeToString(jpeg)),
				anthropic.NewTextBlock(promptImagen(nombre, marca, sku)),
			),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("verificando imagen de %q: %w", sku, err)
	}
	if resp.StopReason == anthropic.StopReasonRefusal {
		return nil, fmt.Errorf("la verificación fue rechazada por el modelo para %q", sku)
	}

	var v Veredicto
	if err := json.Unmarshal([]byte(extraerJSON(textoDe(resp))), &v); err != nil {
		return nil, fmt.Errorf("veredicto ilegible para %q: %w", sku, err)
	}
	if err := validarVeredicto(&v, sku); err != nil {
		return nil, err
	}
	return &v, nil
}

// Comprobar gasta el mínimo posible en verificar que la credencial sirve.
func (a *Anthropic) Comprobar(ctx context.Context) error {
	_, err := a.cli.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     modeloAnthropic,
		MaxTokens: 4,
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("ok"))},
	})
	if err != nil {
		return fmt.Errorf(`no se pudo contactar con la API de Claude: %w

Revisa que ANTHROPIC_API_KEY esté puesta, o usa el modelo local con IA_PROVEEDOR=ollama`, err)
	}
	return nil
}

func textoDe(resp *anthropic.Message) string {
	var b strings.Builder
	for _, bloque := range resp.Content {
		if t, ok := bloque.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}
