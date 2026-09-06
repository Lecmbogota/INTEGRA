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
// base por el mismo camino por el que salió.
func AdaptadorDeCuenta(ctx context.Context, st *store.Store, cif *crypto.Cifrador, cuentaID int64) (channel.Adapter, error) {
	canalCodigo, cifrada, err := st.CredencialesDeCuenta(ctx, cuentaID)
	if err != nil {
		return nil, err
	}
	claro, err := cif.DescifrarTexto(cifrada)
	if err != nil {
		return nil, fmt.Errorf("no se pudo descifrar la credencial de la cuenta %d: %w", cuentaID, err)
	}
	var campos map[string]string
	if err := json.Unmarshal([]byte(claro), &campos); err != nil {
		return nil, fmt.Errorf("credenciales ilegibles de la cuenta %d: %w", cuentaID, err)
	}
	return channel.New(channel.Kind(canalCodigo), channel.Config{
		AccountID:   cuentaID,
		Credentials: campos,
		PersistCredentials: func(ctx context.Context, cred map[string]string) error {
			j, err := json.Marshal(cred)
			if err != nil {
				return err
			}
			cifrada, err := cif.CifrarTexto(string(j))
			if err != nil {
				return err
			}
			return st.ActualizarCredenciales(ctx, cuentaID, cifrada)
		},
	})
}
