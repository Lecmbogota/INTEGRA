package conectores

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mdv/integra/internal/channel"
	"github.com/mdv/integra/internal/crypto"
	"github.com/mdv/integra/internal/store"
)

// AdaptadorDeCuenta construye el adaptador de una cuenta con sus credenciales
// descifradas, y lo deja preparado para guardar las que el canal rote.
//
// Se construye por trabajo y no se cachea: así una credencial reemplazada
// surte efecto en el siguiente envío sin reiniciar el worker. El único
// estado que sobrevive entre trabajos es el que el propio canal pide
// persistir (el refresh token de MercadoLibre), y ese vuelve cifrado a la
// base por el mismo camino por el que salió. Que cada trabajo tenga su
// adaptador es también lo que obliga a que la rotación pase por la base con
// la cuenta bloqueada: ocho adaptadores de la misma cuenta canjeando a la
// vez el mismo refresh token se lo invalidan entre sí.
func AdaptadorDeCuenta(ctx context.Context, st *store.Store, cif *crypto.Cifrador, cuentaID int64) (channel.Adapter, error) {
	canalCodigo, cifrada, err := st.CredencialesDeCuenta(ctx, cuentaID)
	if err != nil {
		return nil, err
	}
	campos, err := descifrarCampos(cif, cuentaID, cifrada)
	if err != nil {
		return nil, err
	}
	ad, err := channel.New(channel.Kind(canalCodigo), channel.Config{
		AccountID:   cuentaID,
		Credentials: campos,
		RotateCredentials: func(ctx context.Context,
			fn func(context.Context, map[string]string) (map[string]string, error)) error {
			// Lo que fn recibe no es lo que se leyó al construir el adaptador
			// sino lo que hay en la base en este instante, ya bloqueado: si
			// otro adaptador acaba de rotar, aquí se ve su token.
			return st.RotarCredenciales(ctx, cuentaID, func(cifrada []byte) ([]byte, error) {
				guardadas, err := descifrarCampos(cif, cuentaID, cifrada)
				if err != nil {
					return nil, err
				}
				nuevas, err := fn(ctx, guardadas)
				if err != nil || nuevas == nil {
					return nil, err
				}
				j, err := json.Marshal(nuevas)
				if err != nil {
					return nil, err
				}
				return cif.CifrarTexto(string(j))
			})
		},
	})
	if err != nil {
		return nil, err
	}

	// El cupo se aplica aquí, en el único sitio por el que pasan todos los
	// adaptadores: el worker dispara tantas llamadas a la vez como
	// concurrencia tenga, y sin freno la publicación inicial del catálogo
	// provoca 429 en cadena que vuelven a la cola y repiten la tormenta.
	rps, burst, err := st.CupoDeCuenta(ctx, cuentaID)
	if err != nil {
		return nil, err
	}
	return conCupo{Adapter: ad, lim: limitadorDe(cuentaID, rps, burst)}, nil
}

// descifrarCampos abre el JSON cifrado de una cuenta. Se lee al construir el
// adaptador y se vuelve a leer, ya con la fila bloqueada, en cada rotación.
func descifrarCampos(cif *crypto.Cifrador, cuentaID int64, cifrada []byte) (map[string]string, error) {
	claro, err := cif.DescifrarTexto(cifrada)
	if err != nil {
		return nil, fmt.Errorf("no se pudo descifrar la credencial de la cuenta %d: %w", cuentaID, err)
	}
	var campos map[string]string
	if err := json.Unmarshal([]byte(claro), &campos); err != nil {
		return nil, fmt.Errorf("credenciales ilegibles de la cuenta %d: %w", cuentaID, err)
	}
	return campos, nil
}
