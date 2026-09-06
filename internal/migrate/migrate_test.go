package migrate

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/mdv/integra/migrations"
)

func TestTrocear(t *testing.T) {
	m, err := trocear("001_core.sql", `-- +goose Up
CREATE TABLE brands (id BIGSERIAL PRIMARY KEY);

-- +goose Down
DROP TABLE brands;
`)
	if err != nil {
		t.Fatalf("error inesperado: %v", err)
	}
	if m.Version != "001" {
		t.Errorf("version = %q", m.Version)
	}
	if !strings.Contains(m.Up, "CREATE TABLE brands") {
		t.Errorf("Up = %q", m.Up)
	}
	if !strings.Contains(m.Down, "DROP TABLE brands") {
		t.Errorf("Down = %q", m.Down)
	}
	// La marca Down no debe colarse dentro de Up.
	if strings.Contains(m.Up, "DROP TABLE") {
		t.Error("la sección Up incluyó sentencias de Down")
	}
}

func TestTrocearErrores(t *testing.T) {
	casos := []struct {
		nombre    string
		fichero   string
		contenido string
		fragmento string
	}{
		{
			nombre:    "sin marca Up",
			fichero:   "001_core.sql",
			contenido: "CREATE TABLE x (id INT);",
			fragmento: "falta la marca",
		},
		{
			nombre:    "Up vacía",
			fichero:   "001_core.sql",
			contenido: "-- +goose Up\n\n-- +goose Down\nDROP TABLE x;",
			fragmento: "está vacía",
		},
		{
			nombre:    "Down antes que Up",
			fichero:   "001_core.sql",
			contenido: "-- +goose Down\nDROP TABLE x;\n-- +goose Up\nCREATE TABLE x (id INT);",
			fragmento: "aparece antes",
		},
		{
			nombre:    "nombre sin versión",
			fichero:   "core.sql",
			contenido: "-- +goose Up\nCREATE TABLE x (id INT);",
			fragmento: "<version>_<descripcion>",
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := trocear(c.fichero, c.contenido)
			if err == nil {
				t.Fatal("se esperaba un error")
			}
			if !strings.Contains(err.Error(), c.fragmento) {
				t.Fatalf("el error debería mencionar %q: %v", c.fragmento, err)
			}
		})
	}
}

func TestCargarOrdenaYDetectaDuplicados(t *testing.T) {
	fsys := fstest.MapFS{
		"003_pricing.sql": {Data: []byte("-- +goose Up\nSELECT 3;")},
		"001_core.sql":    {Data: []byte("-- +goose Up\nSELECT 1;")},
		"002_catalog.sql": {Data: []byte("-- +goose Up\nSELECT 2;")},
	}
	migs, err := Cargar(fsys)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(migs) != 3 {
		t.Fatalf("se esperaban 3 migraciones, hay %d", len(migs))
	}
	// El orden debe ser el de versión, no el del mapa.
	for i, esperado := range []string{"001", "002", "003"} {
		if migs[i].Version != esperado {
			t.Fatalf("posición %d: versión %q, se esperaba %q", i, migs[i].Version, esperado)
		}
	}

	dup := fstest.MapFS{
		"001_core.sql": {Data: []byte("-- +goose Up\nSELECT 1;")},
		"001_otro.sql": {Data: []byte("-- +goose Up\nSELECT 2;")},
	}
	if _, err := Cargar(dup); err == nil || !strings.Contains(err.Error(), "duplicada") {
		t.Fatalf("las versiones duplicadas deberían rechazarse: %v", err)
	}

	if _, err := Cargar(fstest.MapFS{}); err == nil {
		t.Fatal("un sistema de ficheros vacío debería dar error")
	}
}

// Las migraciones reales del proyecto deben trocearse sin errores. Esta prueba
// es la red que impide subir un .sql mal formado.
func TestMigracionesRealesSonValidas(t *testing.T) {
	migs, err := Cargar(migrations.FS)
	if err != nil {
		t.Fatalf("las migraciones del proyecto no cargan: %v", err)
	}
	if len(migs) < 6 {
		t.Fatalf("se esperaban al menos 6 migraciones, hay %d", len(migs))
	}
	for _, m := range migs {
		if m.Down == "" {
			t.Errorf("%s no tiene sección Down: no se podría revertir", m.Nombre)
		}
		if strings.Contains(m.Up, "+goose") {
			t.Errorf("%s: la sección Up contiene una marca goose sin trocear", m.Nombre)
		}
	}
}

func TestLiteralEscapa(t *testing.T) {
	if got := literal("normal"); got != "'normal'" {
		t.Errorf("literal(normal) = %s", got)
	}
	if got := literal("con'comilla"); got != "'con''comilla'" {
		t.Errorf("literal con comilla = %s", got)
	}
}
