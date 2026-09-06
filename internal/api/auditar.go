package api

import (
	"net/http"

	"github.com/mdv/integra/internal/store"
)

// La auditoría de escrituras.
//
// Ante «se vendieron 40 unidades a mitad de precio, ¿quién lo tocó?» la
// respuesta era que no constaba: solo dejaban rastro el login y el alta y la
// edición de usuarios. Ni un cambio de precio, ni una exclusión del catálogo,
// ni una credencial de canal, ni el borrado de una conexión de Odoo. Sin el
// valor de antes tampoco se puede distinguir un dedazo de un fallo del motor
// de precios, que es justo lo que hay que saber para que no se repita.

// auditar deja constancia de una escritura: quién, desde dónde, sobre qué y
// con qué valores antes y después.
//
// El fallo al registrar se anota en el log pero no se propaga: el cambio ya se
// hizo, y devolver un error al operador por un problema del registro le haría
// repetir la operación.
func (s *Server) auditar(r *http.Request, accion, entidad, entidadID string, antes, despues any) {
	if err := s.st.RegistrarAuditoria(r.Context(), usuarioDe(r), accion, entidad, entidadID,
		antes, despues, r.RemoteAddr); err != nil {
		s.log.Error("no se pudo registrar la auditoría",
			"accion", accion, "entidad", entidad, "id", entidadID, "error", err)
	}
}

// usuarioDe saca de la petición quién la hizo, para las tablas que guardan su
// propio created_by además de la fila de auditoría.
func usuarioDe(r *http.Request) *int64 {
	c := ClaimsDeContext(r.Context())
	if c == nil {
		return nil
	}
	id := c.UserID
	return &id
}

// sinCredencial es lo que se guarda en lugar de una credencial de canal o de
// una API key de Odoo.
//
// El registro de auditoría se consulta por API y se conserva indefinidamente:
// volcar ahí el cuerpo de la petición dejaría en claro, en una tabla aparte y
// para siempre, justo lo que se cifra al guardarlo. Queda constancia de que la
// credencial se cambió, nunca de cuál es.
const sinCredencial = "(guardada cifrada, no se registra)"

// cambioAuditable dice si una edición de ficha tocó de verdad el precio o la
// exclusión.
//
// Los punteros se comparan por su valor y no por su dirección: editar solo la
// descripción devuelve un precio idéntico en un puntero distinto, y compararlos
// como structs llenaría la auditoría de cambios de precio que nunca pasaron.
func cambioAuditable(antes, ahora store.EstadoVariante) bool {
	if antes.Excluido != ahora.Excluido {
		return true
	}
	if (antes.Precio == nil) != (ahora.Precio == nil) {
		return true
	}
	return antes.Precio != nil && *antes.Precio != *ahora.Precio
}
