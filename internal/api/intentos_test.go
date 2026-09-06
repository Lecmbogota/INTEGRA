package api

import (
	"testing"
	"time"
)

func frenoDePrueba() (*frenoLogin, *time.Time) {
	t0 := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	reloj := &t0
	f := nuevoFrenoLogin()
	f.ahora = func() time.Time { return *reloj }
	return f, reloj
}

func TestLosPrimerosFallosNoFrenan(t *testing.T) {
	f, _ := frenoDePrueba()
	// Quien se equivoca un par de veces al teclear no puede quedar fuera.
	for i := 0; i < fallosLibres; i++ {
		f.Fallo("1.2.3.4")
		if espera := f.Espera("1.2.3.4"); espera != 0 {
			t.Fatalf("el fallo %d ya frenaba (%v); el margen es %d", i+1, espera, fallosLibres)
		}
	}
}

func TestPasadoElMargenLaEsperaCrece(t *testing.T) {
	f, _ := frenoDePrueba()
	for i := 0; i < fallosLibres+1; i++ {
		f.Fallo("1.2.3.4")
	}
	primera := f.Espera("1.2.3.4")
	if primera <= 0 {
		t.Fatal("pasado el margen tiene que frenar")
	}
	for i := 0; i < 3; i++ {
		f.Fallo("1.2.3.4")
	}
	if segunda := f.Espera("1.2.3.4"); segunda <= primera {
		t.Errorf("la espera debe crecer con los fallos: %v -> %v", primera, segunda)
	}
}

func TestLaEsperaTieneTope(t *testing.T) {
	f, _ := frenoDePrueba()
	for i := 0; i < 100; i++ {
		f.Fallo("1.2.3.4")
	}
	if espera := f.Espera("1.2.3.4"); espera > esperaMaxima {
		t.Errorf("espera %v por encima del tope %v: dejaría fuera a un usuario legítimo", espera, esperaMaxima)
	}
}

func TestEntrarBienBorraElCastigo(t *testing.T) {
	f, _ := frenoDePrueba()
	for i := 0; i < fallosLibres+3; i++ {
		f.Fallo("1.2.3.4")
	}
	if f.Espera("1.2.3.4") == 0 {
		t.Fatal("debería estar frenado")
	}
	f.Acierto("1.2.3.4")
	if espera := f.Espera("1.2.3.4"); espera != 0 {
		t.Errorf("quien acierta no arrastra castigo; esperó %v", espera)
	}
}

func TestElCastigoCaducaConElTiempo(t *testing.T) {
	f, reloj := frenoDePrueba()
	for i := 0; i < fallosLibres+3; i++ {
		f.Fallo("1.2.3.4")
	}
	if f.Espera("1.2.3.4") == 0 {
		t.Fatal("debería estar frenado")
	}

	// Un despiste de hace horas no puede seguir castigando.
	*reloj = reloj.Add(memoriaIntentos + time.Minute)
	if espera := f.Espera("1.2.3.4"); espera != 0 {
		t.Errorf("el castigo debe caducar; esperó %v", espera)
	}
}

func TestSeFrenaPorIPYPorCorreoPorSeparado(t *testing.T) {
	f, _ := frenoDePrueba()

	// Una botnet: muchas IP distintas contra un mismo correo. Por IP no se
	// frenaría nunca; el contador por correo es el que lo detiene.
	for i := 0; i < fallosLibres+2; i++ {
		f.Fallo("ip-distinta-cada-vez", "correo:victima@mdv.com")
	}
	if f.Espera("correo:victima@mdv.com") == 0 {
		t.Error("el conteo por correo debe frenar aunque la IP cambie")
	}

	// Y al revés: una máquina barriendo muchos correos.
	f2, _ := frenoDePrueba()
	for i := 0; i < fallosLibres+2; i++ {
		f2.Fallo("9.9.9.9", "correo:otro-cada-vez@mdv.com")
	}
	if f2.Espera("9.9.9.9") == 0 {
		t.Error("el conteo por IP debe frenar aunque el correo cambie")
	}
}

func TestLimpiarNoDejaCrecerElMapaSinFin(t *testing.T) {
	f, reloj := frenoDePrueba()
	for i := 0; i < 50; i++ {
		f.Fallo("correo:" + string(rune('a'+i%26)) + "@x.com")
	}
	*reloj = reloj.Add(memoriaIntentos + time.Minute)
	f.Limpiar()

	f.mu.Lock()
	quedan := len(f.por)
	f.mu.Unlock()
	if quedan != 0 {
		t.Errorf("quedaron %d entradas caducadas; un ataque con miles de correos agotaría la memoria", quedan)
	}
}
