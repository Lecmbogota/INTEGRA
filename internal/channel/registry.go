package channel

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Config son los datos con los que se construye un adaptador para una cuenta.
// Las credenciales llegan ya descifradas y solo viven en memoria.
type Config struct {
	AccountID   int64
	AccountName string
	Credentials map[string]string
	Settings    map[string]any

	// Sandbox apunta el adaptador al entorno de pruebas del canal.
	Sandbox bool

	// PersistCredentials lo llama el adaptador cuando el canal rota una
	// credencial en mitad del trabajo: MercadoLibre devuelve un refresh token
	// nuevo en cada canje e invalida el anterior, así que si el nuevo no se
	// guarda, el siguiente proceso arranca con uno muerto. Recibe el juego
	// completo ya actualizado. Nulo = no hay dónde guardarlo (pruebas).
	PersistCredentials func(ctx context.Context, cred map[string]string) error
}

// Factory construye un adaptador para una cuenta concreta.
type Factory func(Config) (Adapter, error)

var (
	mu        sync.RWMutex
	factories = map[Kind]Factory{}
)

// Register da de alta un canal. Se llama desde el init() de cada paquete de
// canal, de modo que añadir uno nuevo no obliga a tocar el núcleo.
//
// Registrar dos veces el mismo canal es un error de programación y provoca
// pánico en el arranque, que es cuando conviene descubrirlo.
func Register(k Kind, f Factory) {
	mu.Lock()
	defer mu.Unlock()

	if f == nil {
		panic("channel: factory nula para " + string(k))
	}
	if _, ya := factories[k]; ya {
		panic("channel: el canal " + string(k) + " ya estaba registrado")
	}
	factories[k] = f
}

// New construye el adaptador de un canal.
func New(k Kind, cfg Config) (Adapter, error) {
	mu.RLock()
	f, ok := factories[k]
	mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("canal desconocido %q (registrados: %v)", k, Registered())
	}
	a, err := f(cfg)
	if err != nil {
		return nil, fmt.Errorf("construyendo el adaptador de %s para la cuenta %q: %w",
			k, cfg.AccountName, err)
	}
	return a, nil
}

// Registered devuelve los canales dados de alta, en orden estable.
func Registered() []Kind {
	mu.RLock()
	defer mu.RUnlock()

	out := make([]Kind, 0, len(factories))
	for k := range factories {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// EstaRegistrado indica si un canal tiene adaptador.
func EstaRegistrado(k Kind) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := factories[k]
	return ok
}

// resetParaTest limpia el registro. Solo lo usan las pruebas.
func resetParaTest() {
	mu.Lock()
	defer mu.Unlock()
	factories = map[Kind]Factory{}
}
