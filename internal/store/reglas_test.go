package store

import (
	"context"
	"testing"
)

// Al guardar una regla, el manejador rehace los precios de la cuenta de la
// ruta. Eso solo es verdad si la regla es de esa cuenta: editar por id una
// regla ajena movería los precios de otra cuenta y se recalcularía la
// equivocada, que se quedaría con lo publicado hasta el siguiente horario.
func TestUnaReglaNoSeEditaDesdeOtraCuenta(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)
	cuentaID, _, _, _ := fixturePublicacion(t, st)

	id, err := st.GuardarReglaPrecioCanal(ctx, ReglaPrecioCanal{
		ChannelAccountID: cuentaID, AdjustmentType: "fixed", AdjustmentValue: 5000, Active: true})
	if err != nil {
		t.Fatalf("creando la regla: %v", err)
	}

	_, err = st.GuardarReglaPrecioCanal(ctx, ReglaPrecioCanal{
		ID: id, ChannelAccountID: cuentaID + 1, AdjustmentType: "fixed", AdjustmentValue: 90000, Active: true})
	if err == nil {
		t.Fatal("una regla se dejó editar desde otra cuenta")
	}

	var valor float64
	if err := st.pool.QueryRow(ctx,
		`SELECT adjustment_value FROM channel_price_rules WHERE id = $1`, id).Scan(&valor); err != nil {
		t.Fatal(err)
	}
	if valor != 5000 {
		t.Fatalf("la regla cambió a %.0f desde otra cuenta", valor)
	}
}
