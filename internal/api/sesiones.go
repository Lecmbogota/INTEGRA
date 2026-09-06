package api

import (
	"sync"
	"time"
)

// Caché de vigencia de sesión.
//
// El middleware tiene que saber, en cada petición, si el usuario del token
// sigue existiendo y activo. Preguntárselo a PostgreSQL en todas las rutas
// añadiría una consulta por petición al camino crítico: el panel dispara una
// docena de llamadas al abrirse, y ninguna de ellas necesita una respuesta
// recién leída.
//
// Lo que sí necesita es que una baja surta efecto pronto, no cuando caduque un
// token de 24 horas. Con ttlSesion la ventana pasa de un día a medio minuto, y
// las bajas hechas desde la propia interfaz se aplican al instante porque el
// manejador llama a olvidar(). Lo que queda de retraso es solo para los
// cambios hechos fuera del servidor —la CLI o un UPDATE a mano—, que no tienen
// forma de avisar a este proceso.
const ttlSesion = 30 * time.Second

// estadoSesion es la foto de un usuario, con la hora en que deja de valer.
// Se cachean también los usuarios que ya no existen: si no, el token de un
// usuario borrado provocaría un SELECT en cada una de sus peticiones, que es
// exactamente el caso en el que menos apetece pagar la consulta.
type estadoSesion struct {
	existe bool
	activo bool
	rol    string
	hasta  time.Time
}

func (e estadoSesion) valida() bool { return e.existe && e.activo }

type cacheSesiones struct {
	mu  sync.RWMutex
	m   map[int64]estadoSesion
	ttl time.Duration
	// ahora se inyecta para poder mover el reloj en las pruebas.
	ahora func() time.Time
}

func nuevaCacheSesiones(ttl time.Duration) *cacheSesiones {
	return &cacheSesiones{
		m:     make(map[int64]estadoSesion),
		ttl:   ttl,
		ahora: time.Now,
	}
}

// leer devuelve el estado guardado si aún no ha caducado.
func (c *cacheSesiones) leer(id int64) (estadoSesion, bool) {
	c.mu.RLock()
	e, hay := c.m[id]
	c.mu.RUnlock()
	if !hay || c.ahora().After(e.hasta) {
		return estadoSesion{}, false
	}
	return e, true
}

// guardar anota el estado recién leído de la base.
func (c *cacheSesiones) guardar(id int64, e estadoSesion) {
	e.hasta = c.ahora().Add(c.ttl)

	c.mu.Lock()
	defer c.mu.Unlock()
	// El mapa está acotado por el número de usuarios reales —los identificadores
	// vienen de tokens firmados, no se pueden inventar—, pero una purga barata
	// cuando crece evita que las entradas muertas se acumulen para siempre.
	if len(c.m) > 512 {
		ahora := c.ahora()
		for k, v := range c.m {
			if ahora.After(v.hasta) {
				delete(c.m, k)
			}
		}
	}
	c.m[id] = e
}

// olvidar descarta lo guardado de un usuario para que la siguiente petición
// vuelva a preguntar. Lo llama el manejador que edita usuarios: dentro del
// propio servidor no hay motivo para esperar al vencimiento.
func (c *cacheSesiones) olvidar(id int64) {
	c.mu.Lock()
	delete(c.m, id)
	c.mu.Unlock()
}
