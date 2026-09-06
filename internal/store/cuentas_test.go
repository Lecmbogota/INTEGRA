package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

// El refresh token de MercadoLibre es de un solo uso, así que quien canjea
// segundo tiene que ver lo que guardó el primero, esté en este proceso o en
// el del panel. RotarCredenciales lo garantiza bloqueando la fila durante
// toda la rotación; aquí se lanzan dos a la vez contra el esquema real y se
// comprueba que la segunda recibe la credencial que dejó la primera, no la
// que había antes de empezar.

func TestLaSegundaRotacionVeLoQueGuardoLaPrimera(t *testing.T) {
	ctx := context.Background()
	st := abrirStore(t)

	var cuentaID int64
	err := st.pool.QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active)
		SELECT NULL, id, 'cuenta-rotacion-test', 'v0'::bytea, false FROM channels LIMIT 1
		RETURNING id`).Scan(&cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.pool.Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
	})

	// La primera entra, avisa de que tiene la fila y se queda un rato dentro,
	// como si estuviera hablando con el canal. La segunda arranca en cuanto
	// recibe el aviso y solo puede seguir cuando la primera suelta la fila.
	dentro := make(chan struct{})
	var primera, segunda error
	var vistoPorLaSegunda []byte
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		primera = st.RotarCredenciales(ctx, cuentaID, func([]byte) ([]byte, error) {
			close(dentro)
			time.Sleep(300 * time.Millisecond)
			return []byte("v1"), nil
		})
	}()
	go func() {
		defer wg.Done()
		<-dentro
		segunda = st.RotarCredenciales(ctx, cuentaID, func(cifrada []byte) ([]byte, error) {
			vistoPorLaSegunda = cifrada
			return nil, nil // nada que guardar: adopta lo que hay
		})
	}()
	wg.Wait()

	if primera != nil || segunda != nil {
		t.Fatalf("primera=%v segunda=%v", primera, segunda)
	}
	if string(vistoPorLaSegunda) != "v1" {
		t.Errorf("la segunda rotación vio %q; debía ver lo que guardó la primera", vistoPorLaSegunda)
	}
	var final []byte
	if err := st.pool.QueryRow(ctx,
		`SELECT credentials_enc FROM channel_accounts WHERE id = $1`, cuentaID).Scan(&final); err != nil {
		t.Fatal(err)
	}
	if string(final) != "v1" {
		t.Errorf("quedó guardado %q: devolver nula no debe tocar la fila", final)
	}
}

func TestRotarCredencialesDeUnaCuentaInexistenteNoLlamaAlCanje(t *testing.T) {
	st := abrirStore(t)
	llamado := false
	err := st.RotarCredenciales(context.Background(), -1, func([]byte) ([]byte, error) {
		llamado = true
		return nil, nil
	})
	if err == nil || llamado {
		t.Errorf("err=%v llamado=%v; sin cuenta no hay nada que rotar", err, llamado)
	}
}
