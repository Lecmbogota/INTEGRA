package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Servir la interfaz desde el mismo binario.
//
// En desarrollo el frontend lo sirve Vite en otro puerto y hablan por CORS. En
// producción eso obliga a montar un segundo servidor, publicar dos dominios y
// abrir CORS de verdad: tres cosas que pueden salir mal para no ganar nada.
// Sirviendo los archivos ya compilados desde aquí, el despliegue es un único
// origen, la cookie de sesión no cruza dominios y CORS no llega a activarse.

// servirInterfaz devuelve el manejador de los archivos estáticos, o nil si el
// directorio no existe (que es lo normal en desarrollo).
func servirInterfaz(dir string) http.Handler {
	if dir == "" {
		return nil
	}
	indice := filepath.Join(dir, "index.html")
	if _, err := os.Stat(indice); err != nil {
		return nil
	}
	archivos := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limpio := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		// Rutas de la aplicación (las que no son un archivo real) devuelven el
		// index: la navegación del frontend ocurre en el navegador, así que
		// recargar en una sección interna tiene que seguir funcionando en vez
		// de dar 404.
		if datos, err := os.Stat(filepath.Join(dir, limpio)); err != nil || datos.IsDir() {
			// index.html sin caché: es lo que apunta al resto de archivos, y
			// si el navegador lo guarda, tras un despliegue pide recursos que
			// ya no existen y la pantalla queda en blanco.
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, indice)
			return
		}
		// El resto lleva el hash del contenido en el nombre, así que puede
		// guardarse indefinidamente: cambiar el código cambia el nombre.
		if strings.HasPrefix(limpio, "assets"+string(filepath.Separator)) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		archivos.ServeHTTP(w, r)
	})
}

// existeInterfaz dice si hay algo que servir, para no anunciar en el log una
// interfaz que no está.
func existeInterfaz(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := fs.Stat(os.DirFS(dir), "index.html")
	return err == nil
}
