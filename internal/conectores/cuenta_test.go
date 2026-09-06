package conectores_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/conectores"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/store"

	_ "github.com/mdv/integra/internal/conectores/mercadolibre"
)

// El camino entero del worker: ocho trabajos de la misma cuenta de
// MercadoLibre construyen ocho adaptadores desde la base, los ocho se
// encuentran el access token caducado a la vez y ML solo honra el primer
// canje del refresh token. Sin la fila bloqueada y releída, siete fallarían
// con invalid_grant y el que guardara último podría dejar la cuenta muerta.
// La prueba usa el esquema real y una API de ML falsa que, como la de
// verdad, invalida cada refresh token al canjearlo.
func TestOchoTrabajosDeLaMismaCuentaCanjeanElRefreshTokenUnaSolaVez(t *testing.T) {
	dsn := os.Getenv("INTEGRA_DATABASE_URL")
	if dsn == "" {
		t.Skip("sin INTEGRA_DATABASE_URL: test de integración omitido")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatalf("abriendo el store: %v", err)
	}
	t.Cleanup(st.Close)
	cif, err := crypto.Nuevo(bytes.Repeat([]byte{7}, crypto.KeySize))
	if err != nil {
		t.Fatal(err)
	}

	ml := nuevoServidorML(t)
	viejo := conectores.URLBaseML
	conectores.URLBaseML = ml.URL
	t.Cleanup(func() { conectores.URLBaseML = viejo })
	conectores.OlvidarCupos()
	t.Cleanup(conectores.OlvidarCupos)

	claro, _ := json.Marshal(map[string]string{
		"app_id": "1", "app_secret": "s", "refresh_token": "TG-inicial",
		"access_token": "caducado", "url_seguimiento": "https://envios.example/{guia}",
	})
	cifrada, err := cif.CifrarTexto(string(claro))
	if err != nil {
		t.Fatal(err)
	}
	// Cupo alto: aquí se prueba el canje, no el limitador.
	var cuentaID int64
	err = st.Pool().QueryRow(ctx, `
		INSERT INTO channel_accounts (brand_id, channel_id, name, credentials_enc, active,
		                              rate_limit_rps, rate_limit_burst)
		SELECT NULL, id, 'cuenta-canje-test', $1, true, 1000, 100
		FROM channels WHERE code = 'mercadolibre'
		RETURNING id`, cifrada).Scan(&cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, `DELETE FROM channel_accounts WHERE id = $1`, cuentaID)
	})

	const trabajos = 8
	errs := make([]error, trabajos)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ad, err := conectores.AdaptadorDeCuenta(ctx, st, cif, cuentaID)
			if err != nil {
				errs[i] = err
				return
			}
			_, errs[i] = ad.FetchStatus(ctx, []channel.ExternalRef{{ListingID: "MCO1"}})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("trabajo %d: %v", i, err)
		}
	}
	if ml.canjes != 1 || ml.reusos != 0 {
		t.Errorf("canjes=%d reusos=%d; los ocho trabajos comparten cuenta y debían canjear una sola vez",
			ml.canjes, ml.reusos)
	}

	// Y en la base queda el juego del único canje, con lo que no rota intacto.
	_, cifradaFinal, err := st.CredencialesDeCuenta(ctx, cuentaID)
	if err != nil {
		t.Fatal(err)
	}
	claroFinal, err := cif.DescifrarTexto(cifradaFinal)
	if err != nil {
		t.Fatal(err)
	}
	var final map[string]string
	if err := json.Unmarshal([]byte(claroFinal), &final); err != nil {
		t.Fatal(err)
	}
	if final["refresh_token"] != "TG-nuevo-1" || final["access_token"] != "APP_USR-nuevo-1" {
		t.Errorf("quedó guardado refresh=%q access=%q; debía ser el juego del único canje",
			final["refresh_token"], final["access_token"])
	}
	if final["url_seguimiento"] != "https://envios.example/{guia}" || final["app_id"] != "1" {
		t.Errorf("el canje perdió campos que no rotan: %v", final)
	}
}

// servidorML imita lo justo de api.mercadolibre.com: el canje del token, de
// un solo uso como el de verdad, y una consulta cualquiera que exige un
// token vigente.
type servidorML struct {
	*httptest.Server
	mu        sync.Mutex
	canjes    int
	reusos    int
	canjeados map[string]bool
}

func nuevoServidorML(t *testing.T) *servidorML {
	t.Helper()
	s := &servidorML{canjeados: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		rt := r.Form.Get("refresh_token")
		s.mu.Lock()
		if s.canjeados[rt] {
			s.reusos++
			s.mu.Unlock()
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Error validating grant"}`)
			return
		}
		s.canjes++
		s.canjeados[rt] = true
		n := strconv.Itoa(s.canjes)
		s.mu.Unlock()
		// El canje de verdad tarda lo que tarda un viaje a ML; contra un
		// servidor local termina antes de que el resto de trabajos lea la
		// cuenta y la carrera no llega a producirse. Se le devuelve su
		// duración para que los ocho se encuentren el token caducado a la vez.
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"APP_USR-nuevo-`+n+`","token_type":"bearer",`+
			`"expires_in":10800,"refresh_token":"TG-nuevo-`+n+`","user_id":123}`)
	})
	mux.HandleFunc("GET /items", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer APP_USR-") {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"message":"invalid token","error":"unauthorized"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"code":200,"body":{"id":"MCO1","status":"active","price":100,"available_quantity":1}}]`)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}
