package api

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Freno de fuerza bruta contra el login.
//
// POST /api/auth/login es público por necesidad y no tenía ningún límite: con
// bcrypt de coste 10 (unos 70 ms) un atacante prueba del orden de catorce
// contraseñas por segundo y por núcleo, sin recibir un solo 429. Contra el
// mínimo de contraseña que acepta el sistema, un diccionario acaba entrando
// en horas; y cada intento, además, gasta CPU del servidor en bcrypt.
//
// El freno es por IP y por correo a la vez, porque cada uno tapa el hueco del
// otro: por IP no frena una botnet repartida contra una sola cuenta, y por
// correo no frena a quien barre muchas cuentas desde una máquina.

const (
	// Fallos tolerados antes de empezar a frenar. Deja margen para el error
	// humano corriente sin dar espacio a un diccionario.
	fallosLibres = 5
	// Tope de la espera: más allá, el atacante ya está frenado y alargarlo
	// solo facilitaría dejar fuera a un usuario legítimo.
	esperaMaxima = 5 * time.Minute
	// Cuánto se recuerda un intento fallido sin actividad nueva.
	memoriaIntentos = 15 * time.Minute
)

type intento struct {
	fallos int
	ultimo time.Time
}

type frenoLogin struct {
	mu    sync.Mutex
	por   map[string]*intento
	ahora func() time.Time
}

// frenoDeLogin es del paquete y no del Server porque el proceso sirve una
// sola instancia: el estado que protege es el del host, no el de un objeto.
var frenoDeLogin = nuevoFrenoLogin()

func nuevoFrenoLogin() *frenoLogin {
	return &frenoLogin{por: map[string]*intento{}, ahora: time.Now}
}

// Espera indica cuánto falta para que esta clave pueda volver a intentarlo.
// Cero significa que puede pasar.
func (f *frenoLogin) Espera(clave string) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	i, ok := f.por[clave]
	if !ok {
		return 0
	}
	ahora := f.ahora()
	// Un intento viejo se olvida: si no, un usuario despistado hace meses
	// arrastraría el castigo para siempre.
	if ahora.Sub(i.ultimo) > memoriaIntentos {
		delete(f.por, clave)
		return 0
	}
	if i.fallos <= fallosLibres {
		return 0
	}
	espera := f.castigo(i.fallos)
	if transcurrido := ahora.Sub(i.ultimo); transcurrido >= espera {
		return 0
	} else {
		return espera - transcurrido
	}
}

// castigo crece exponencialmente: 1 s, 2 s, 4 s… hasta el tope.
func (f *frenoLogin) castigo(fallos int) time.Duration {
	espera := time.Second
	for i := fallosLibres + 1; i < fallos; i++ {
		espera *= 2
		if espera >= esperaMaxima {
			return esperaMaxima
		}
	}
	return espera
}

func (f *frenoLogin) Fallo(claves ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	ahora := f.ahora()
	for _, c := range claves {
		i, ok := f.por[c]
		if !ok || ahora.Sub(i.ultimo) > memoriaIntentos {
			i = &intento{}
			f.por[c] = i
		}
		i.fallos++
		i.ultimo = ahora
	}
}

// Acierto borra el historial: quien entra bien no arrastra castigo.
func (f *frenoLogin) Acierto(claves ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range claves {
		delete(f.por, c)
	}
}

// Limpiar descarta lo caducado, para que el mapa no crezca sin fin bajo un
// ataque que use miles de correos distintos.
func (f *frenoLogin) Limpiar() {
	f.mu.Lock()
	defer f.mu.Unlock()

	ahora := f.ahora()
	for c, i := range f.por {
		if ahora.Sub(i.ultimo) > memoriaIntentos {
			delete(f.por, c)
		}
	}
}

// ipDe saca la dirección del cliente sin el puerto, que cambia en cada
// conexión y haría inútil cualquier conteo por IP.
func ipDe(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
