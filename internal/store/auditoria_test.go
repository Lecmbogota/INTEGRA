package store

import (
	"encoding/json"
	"testing"

	"github.com/mdv/integra/internal/auth"
)

// El registro de auditoría no guardaba absolutamente nada, y no por falta de
// llamadas: todas pasan r.RemoteAddr —"10.0.0.4:53124"— a una columna inet, el
// INSERT reventaba en el cast, y como el registro se escribe sin dejar que su
// fallo tumbe la operación auditada, el error se tragaba en silencio.
func TestLaAuditoriaSeGuardaAunqueLaDireccionTraigaPuerto(t *testing.T) {
	st, ctx := abrir(t)
	usuarioID := usuarioDePrueba(t, st, ctx, auth.RolOperator).ID

	antes := map[string]any{"precio": 400000}
	despues := map[string]any{"precio": 4000}
	if err := st.RegistrarAuditoria(ctx, &usuarioID, "update", "zz_prueba_auditoria", "1",
		antes, despues, "10.0.0.4:53124"); err != nil {
		t.Fatalf("registrando la auditoría: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM audit_logs WHERE entity = 'zz_prueba_auditoria'`)
	})

	logs, total, err := st.ListarAuditoria(ctx, FiltroAuditoria{Entity: "zz_prueba_auditoria"})
	if err != nil {
		t.Fatalf("consultando la auditoría: %v", err)
	}
	if total != 1 {
		t.Fatalf("el evento no quedó registrado: total=%d", total)
	}
	if logs[0].IP == nil || *logs[0].IP != "10.0.0.4" {
		t.Errorf("la IP no se guardó limpia: %v", logs[0].IP)
	}

	// Sin el valor de antes no se puede distinguir un dedazo de un fallo del
	// motor de precios, que es justo para lo que sirve el registro.
	var previo map[string]any
	if err := json.Unmarshal(logs[0].Before, &previo); err != nil {
		t.Fatalf("el valor anterior no se guardó legible: %v", err)
	}
	if previo["precio"] != float64(400000) {
		t.Errorf("el valor anterior no es el que era: %v", previo)
	}
}

// Una dirección que no se puede leer no puede tumbar el registro: quedarse sin
// la IP es mucho menos malo que quedarse sin el evento.
func TestUnaDireccionIlegibleNoImpideRegistrarElEvento(t *testing.T) {
	st, ctx := abrir(t)
	usuarioID := usuarioDePrueba(t, st, ctx, auth.RolOperator).ID

	if err := st.RegistrarAuditoria(ctx, &usuarioID, "update", "zz_prueba_auditoria", "2",
		nil, nil, "no-es-una-direccion"); err != nil {
		t.Fatalf("registrando la auditoría: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM audit_logs WHERE entity = 'zz_prueba_auditoria'`)
	})

	_, total, err := st.ListarAuditoria(ctx, FiltroAuditoria{Entity: "zz_prueba_auditoria", EntityID: "2"})
	if err != nil {
		t.Fatalf("consultando la auditoría: %v", err)
	}
	if total != 1 {
		t.Fatalf("el evento se perdió por culpa de la dirección: total=%d", total)
	}
}
