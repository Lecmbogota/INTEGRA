package api

import (
	"testing"
	"time"
)

// La caché es lo que hace asumible comprobar la vigencia de la sesión en cada
// petición, así que su comportamiento en el tiempo importa tanto como el del
// middleware: si no caducara, una baja no se notaría nunca.

func TestLaCacheDeSesionesCaduca(t *testing.T) {
	c := nuevaCacheSesiones(30 * time.Second)
	reloj := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.ahora = func() time.Time { return reloj }

	c.guardar(7, estadoSesion{existe: true, activo: true, rol: "operator"})

	if e, hay := c.leer(7); !hay || !e.valida() || e.rol != "operator" {
		t.Fatalf("lo recién guardado debería leerse: hay=%v estado=%+v", hay, e)
	}

	reloj = reloj.Add(29 * time.Second)
	if _, hay := c.leer(7); !hay {
		t.Error("a los 29 s todavía debería valer")
	}

	reloj = reloj.Add(2 * time.Second)
	if _, hay := c.leer(7); hay {
		t.Error("pasados los 30 s la entrada debía caducar y obligar a releer la base")
	}
}

func TestOlvidarFuerzaLaRelectura(t *testing.T) {
	c := nuevaCacheSesiones(30 * time.Second)
	c.guardar(7, estadoSesion{existe: true, activo: true, rol: "admin"})

	c.olvidar(7)

	if _, hay := c.leer(7); hay {
		t.Error("después de olvidar, la siguiente petición tiene que ir a la base")
	}
}

// Un usuario borrado se cachea igual que uno vivo: si no, su token seguiría
// provocando un SELECT por petición hasta que caducara.
func TestSeCacheaTambienElUsuarioQueYaNoExiste(t *testing.T) {
	c := nuevaCacheSesiones(30 * time.Second)
	c.guardar(7, estadoSesion{})

	e, hay := c.leer(7)
	if !hay {
		t.Fatal("el estado de un usuario inexistente también debe guardarse")
	}
	if e.valida() {
		t.Error("un usuario que no existe no puede dar por válida la sesión")
	}
}

func TestUnUsuarioDesactivadoNoValida(t *testing.T) {
	e := estadoSesion{existe: true, activo: false, rol: "admin"}
	if e.valida() {
		t.Error("existir no basta: un usuario inactivo no tiene sesión válida")
	}
}

// El mapa no puede crecer sin fin. Las entradas caducadas se barren cuando se
// pasa del umbral, y las vivas tienen que sobrevivir a esa barrida.
func TestLaPurgaTiraLoCaducadoYRespetaLoVivo(t *testing.T) {
	c := nuevaCacheSesiones(30 * time.Second)
	reloj := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	c.ahora = func() time.Time { return reloj }

	for i := int64(0); i < 600; i++ {
		c.guardar(i, estadoSesion{existe: true, activo: true, rol: "viewer"})
	}
	reloj = reloj.Add(time.Minute)

	c.guardar(9001, estadoSesion{existe: true, activo: true, rol: "admin"})

	if len(c.m) != 1 {
		t.Errorf("quedan %d entradas; solo la recién guardada debía sobrevivir", len(c.m))
	}
	if _, hay := c.leer(9001); !hay {
		t.Error("la purga se llevó por delante la entrada viva")
	}
}
