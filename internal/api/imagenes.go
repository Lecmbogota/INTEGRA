package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdv/integra/internal/imagen"
	"github.com/mdv/integra/internal/store"
	"github.com/mdv/integra/internal/webimagenes"
)

// registrarImagenes añade las rutas del banco de imágenes.
func (s *Server) registrarImagenes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/productos/{id}/imagenes", s.listarImagenes)
	mux.HandleFunc("POST /api/productos/{id}/imagenes", s.subirImagen)
	mux.HandleFunc("POST /api/productos/{id}/imagenes/buscar", s.buscarImagenesWeb)
	mux.HandleFunc("DELETE /api/productos/{id}/imagenes/{imagenID}", s.quitarImagen)
	mux.HandleFunc("POST /api/productos/{id}/imagenes/{imagenID}/principal", s.principalImagen)
	// El banco entero, para la mediateca: lo que no se ve producto a producto.
	mux.HandleFunc("GET /api/imagenes", s.banco)
	mux.HandleFunc("DELETE /api/imagenes/{id}", s.borrarDelBanco)
	// El fichero se sirve por hash, no por identificador: así la URL es
	// inmutable y se puede cachear para siempre.
	mux.HandleFunc("GET /imagenes/{sha}", s.servirImagen)
	mux.HandleFunc("GET /imagenes/{sha}/{variante}", s.servirImagen)
}

// banco lista el banco de imágenes con filtro, búsqueda y página.
func (s *Server) banco(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limite, _ := strconv.Atoi(q.Get("limite"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	pg, err := s.st.Banco(r.Context(), q.Get("filtro"), q.Get("q"), limite, offset)
	if err != nil {
		escribir(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	escribir(w, http.StatusOK, pg)
}

// borrarDelBanco elimina del disco una imagen que no usa ningún producto.
// La base decide si se puede (se niega si alguien la usa); los ficheros se
// quitan después, y si alguno ya no estaba no pasa nada.
func (s *Server) borrarDelBanco(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	rutas, err := s.st.BorrarDelBanco(r.Context(), id)
	if err != nil {
		escribir(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	if s.almacen != nil {
		for _, ruta := range rutas {
			_ = s.almacen.Borrar(ruta)
		}
	}
	var usuario *int64
	if c := ClaimsDeContext(r.Context()); c != nil {
		usuario = &c.UserID
	}
	_ = s.st.RegistrarAuditoria(r.Context(), usuario, "delete", "imagenes",
		strconv.FormatInt(id, 10), nil, map[string]any{"ficheros": len(rutas)}, r.RemoteAddr)
	escribir(w, http.StatusOK, map[string]string{"estado": "borrada"})
}

// productoDesdeRuta acepta el identificador de variante que usa el resto de la
// interfaz y lo traduce a producto, que es a lo que cuelgan las imágenes.
func (s *Server) productoDesdeRuta(r *http.Request) (int64, error) {
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("identificador inválido")
	}
	return s.st.ProductoDeVariante(r.Context(), varianteID)
}

func (s *Server) listarImagenes(w http.ResponseWriter, r *http.Request) {
	prodID, err := s.productoDesdeRuta(r)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	imgs, err := s.st.ImagenesDeProducto(r.Context(), prodID)
	if err != nil {
		s.fallo(w, err)
		return
	}
	escribir(w, http.StatusOK, decorar(imgs))
}

// subirImagen recibe un fichero, lo procesa y lo asocia al producto.
func (s *Server) subirImagen(w http.ResponseWriter, r *http.Request) {
	if s.almacen == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "el banco de imágenes no está configurado"})
		return
	}
	prodID, err := s.productoDesdeRuta(r)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Se acota la lectura para que un fichero enorme no agote la memoria del
	// servidor antes de llegar a la validación.
	if err := r.ParseMultipartForm(imagen.MaxBytesEntrada); err != nil {
		escribir(w, http.StatusBadRequest,
			map[string]string{"error": "no se pudo leer el formulario: " + err.Error()})
		return
	}
	fichero, cabecera, err := r.FormFile("archivo")
	if err != nil {
		escribir(w, http.StatusBadRequest,
			map[string]string{"error": "falta el campo 'archivo'"})
		return
	}
	defer fichero.Close()

	datos, err := io.ReadAll(io.LimitReader(fichero, imagen.MaxBytesEntrada+1))
	if err != nil {
		s.fallo(w, err)
		return
	}

	res, err := s.almacen.Ingerir(datos, imagen.VariantesPorDefecto)
	if err != nil {
		// Estos errores sí van al cliente: dicen exactamente qué pasa con el
		// fichero que acaba de elegir.
		escribir(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	derivadas := make([]struct {
		Variante, Ruta, Formato string
		Ancho, Alto, Bytes      int
	}, 0, len(res.Derivadas))
	for _, d := range res.Derivadas {
		derivadas = append(derivadas, struct {
			Variante, Ruta, Formato string
			Ancho, Alto, Bytes      int
		}{d.Variante, d.Ruta, d.Info.Formato, d.Info.Ancho, d.Info.Alto, d.Info.Bytes})
	}

	imgID, err := s.st.RegistrarImagen(r.Context(),
		res.Original.SHA256, res.RutaOrig, res.Original.Formato,
		res.Original.Ancho, res.Original.Alto, res.Original.Bytes,
		"subida", cabecera.Filename, derivadas)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if err := s.st.AsociarImagen(r.Context(), prodID, imgID); err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusCreated, map[string]any{
		"id": imgID, "sha256": res.Original.SHA256,
		"ancho": res.Original.Ancho, "alto": res.Original.Alto,
		"formato": res.Original.Formato, "bytes": res.Original.Bytes,
		"publicable": imagen.Publicable(res.Original),
		"problemas":  imagen.ValidarTodos(res.Original),
	})
}

// buscarImagenesWeb busca fotos del producto en internet por SKU y descarga
// varias candidatas al banco, para que una persona elija cuáles se quedan.
//
// Solo entran las que miden al menos 500 px por lado (el mínimo que exigen
// los marketplaces); las demás se cuentan como descartadas. Las fotos de
// internet suelen ser del fabricante: quien publica es responsable de
// verificar que puede usarlas.
func (s *Server) buscarImagenesWeb(w http.ResponseWriter, r *http.Request) {
	if s.almacen == nil {
		escribir(w, http.StatusServiceUnavailable,
			map[string]string{"error": "el banco de imágenes no está configurado"})
		return
	}
	varianteID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	prodID, err := s.st.ProductoDeVariante(r.Context(), varianteID)
	if err != nil {
		escribir(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	var cuerpo struct {
		Consulta string `json:"consulta"`
		Max      int    `json:"max"`
	}
	// El cuerpo es opcional: sin él se busca por SKU y marca con 5 resultados.
	_ = json.NewDecoder(r.Body).Decode(&cuerpo)
	if cuerpo.Max <= 0 {
		cuerpo.Max = 5
	}
	if cuerpo.Max > 10 {
		cuerpo.Max = 10
	}

	sku, nombre, marca, shasExistentes, err := s.st.DatosBusquedaImagen(r.Context(), varianteID)
	if err != nil {
		s.fallo(w, err)
		return
	}
	consulta := strings.TrimSpace(cuerpo.Consulta)
	if consulta == "" {
		// El SKU es el término más certero: es como los fabricantes y las
		// tiendas titulan sus fichas. La marca acota los códigos ambiguos.
		if strings.TrimSpace(sku) != "" {
			consulta = strings.TrimSpace(sku + " " + marca)
		} else {
			consulta = strings.TrimSpace(nombre + " " + marca)
		}
	}

	yaTenia := make(map[string]bool, len(shasExistentes))
	for _, sha := range shasExistentes {
		yaTenia[sha] = true
	}

	buscador := webimagenes.NuevoBuscador()
	// Se piden más candidatas de las que se quieren guardar: parte de las
	// descargas falla (hotlinks bloqueados, ficheros rotos, duplicados).
	// 600 px es el mínimo más alto de los cuatro canales (Falabella): lo que
	// baje de ahí serviría solo en algunos y llenaría el banco de medias tintas.
	candidatas, err := buscador.Buscar(r.Context(), consulta, 600, cuerpo.Max*4)
	if err != nil {
		escribir(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	agregadas, descartadas, err := s.ingerirCandidatas(r.Context(), buscador, prodID, candidatas, cuerpo.Max, yaTenia)
	if err != nil {
		s.fallo(w, err)
		return
	}

	escribir(w, http.StatusOK, map[string]any{
		"consulta":    consulta,
		"encontradas": len(candidatas),
		"agregadas":   agregadas,
		"descartadas": descartadas,
	})
}

// ingerirCandidatas descarga candidatas hasta reunir max válidas (≥500 px y
// no repetidas), las procesa en el banco y las asocia al producto. Los fallos
// de descarga o proceso se cuentan como descartadas, no abortan el lote.
func (s *Server) ingerirCandidatas(ctx context.Context, buscador *webimagenes.Buscador,
	prodID int64, candidatas []webimagenes.Candidata, max int, yaTenia map[string]bool) ([]map[string]any, int, error) {

	agregadas := []map[string]any{}
	descartadas := 0
	for _, c := range candidatas {
		if len(agregadas) >= max {
			break
		}
		datos, err := buscador.Descargar(ctx, c.URL, imagen.MaxBytesEntrada)
		if err != nil {
			descartadas++
			continue
		}
		res, err := s.almacen.Ingerir(datos, imagen.VariantesPorDefecto)
		if err != nil {
			descartadas++
			continue
		}
		if res.Original.Ancho < 600 || res.Original.Alto < 600 || yaTenia[res.Original.SHA256] {
			descartadas++
			continue
		}
		yaTenia[res.Original.SHA256] = true

		derivadas := make([]struct {
			Variante, Ruta, Formato string
			Ancho, Alto, Bytes      int
		}, 0, len(res.Derivadas))
		for _, d := range res.Derivadas {
			derivadas = append(derivadas, struct {
				Variante, Ruta, Formato string
				Ancho, Alto, Bytes      int
			}{d.Variante, d.Ruta, d.Info.Formato, d.Info.Ancho, d.Info.Alto, d.Info.Bytes})
		}
		imgID, err := s.st.RegistrarImagen(ctx,
			res.Original.SHA256, res.RutaOrig, res.Original.Formato,
			res.Original.Ancho, res.Original.Alto, res.Original.Bytes,
			"web", c.Fuente, derivadas)
		if err != nil {
			return nil, descartadas, err
		}
		if err := s.st.AsociarImagen(ctx, prodID, imgID); err != nil {
			return nil, descartadas, err
		}
		agregadas = append(agregadas, map[string]any{
			"id": imgID, "sha256": res.Original.SHA256,
			"ancho": res.Original.Ancho, "alto": res.Original.Alto,
			"fuente": c.Redirect,
		})
	}
	return agregadas, descartadas, nil
}

func (s *Server) quitarImagen(w http.ResponseWriter, r *http.Request) {
	prodID, err := s.productoDesdeRuta(r)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	imgID, err := strconv.ParseInt(r.PathValue("imagenID"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	if err := s.st.DesasociarImagen(r.Context(), prodID, imgID); err != nil {
		escribir(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	// La cola de atención depende de qué fotos tiene el producto y cuál es la
	// portada: quitar la última deja «sin fotos», y quitar la que la IA marcó
	// como equivocada tiene que borrar ese aviso en el acto.
	_ = s.st.RecalcularAtencion(r.Context())
	escribir(w, http.StatusOK, map[string]string{"estado": "quitada"})
}

func (s *Server) principalImagen(w http.ResponseWriter, r *http.Request) {
	prodID, err := s.productoDesdeRuta(r)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	imgID, err := strconv.ParseInt(r.PathValue("imagenID"), 10, 64)
	if err != nil {
		escribir(w, http.StatusBadRequest, map[string]string{"error": "identificador inválido"})
		return
	}
	if err := s.st.MarcarPrincipal(r.Context(), prodID, imgID); err != nil {
		escribir(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	// «La portada no cuadra» habla de la portada: al cambiarla, el aviso que
	// pesaba sobre la anterior deja de tener sentido. Sin este recálculo se
	// quedaba rancio y nadie sabía por qué seguía ahí.
	_ = s.st.RecalcularAtencion(r.Context())
	escribir(w, http.StatusOK, map[string]string{"estado": "principal"})
}

// servirImagen entrega el fichero desde el almacén.
func (s *Server) servirImagen(w http.ResponseWriter, r *http.Request) {
	if s.almacen == nil {
		http.Error(w, "banco de imágenes no configurado", http.StatusServiceUnavailable)
		return
	}
	// La extension se admite en cualquiera de los dos tramos y se descarta:
	// las URL que se publican la llevan al final porque algunos canales
	// deducen el tipo del fichero por ahi —WordPress rechaza la descarga sin
	// ella— pero lo que identifica al fichero es el hash, no el nombre.
	sha := recortarExtension(r.PathValue("sha"))
	variante := recortarExtension(r.PathValue("variante"))

	ruta, err := s.st.RutaDeImagen(r.Context(), sha, variante)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	datos, err := s.almacen.Leer(ruta)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", imagen.TipoMIME(ruta))
	// El contenido de una URL con hash nunca cambia, así que se puede cachear
	// indefinidamente. Es lo que hace barato que un canal la descargue muchas
	// veces.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+sha+`"`)
	if r.Header.Get("If-None-Match") == `"`+sha+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(datos)
}

// decorar añade a cada imagen su URL y en qué canales sirve.
func decorar(imgs []store.ImagenGuardada) []map[string]any {
	out := make([]map[string]any, 0, len(imgs))
	for _, i := range imgs {
		inf := imagen.Info{
			Formato: i.Formato, Ancho: i.Ancho, Alto: i.Alto, Bytes: i.Bytes,
		}
		out = append(out, map[string]any{
			"id": i.ID, "sha256": i.SHA256,
			"formato": i.Formato, "ancho": i.Ancho, "alto": i.Alto, "bytes": i.Bytes,
			"origen": i.Origen, "posicion": i.Posicion, "principal": i.Principal,
			"url":            "/imagenes/" + i.SHA256 + "/miniatura_300",
			"url_publicable": "/imagenes/" + i.SHA256 + "/cuadrada_1200",
			"publicable":     imagen.Publicable(inf),
			"problemas":      imagen.ValidarTodos(inf),
		})
	}
	return out
}

// recortarExtension quita la extension de imagen de un tramo de la ruta.
//
// Se acepta cualquiera de las tres que sabe servir el banco, y no solo .jpg,
// para que una URL publicada antes de un cambio de formato siga resolviendo:
// el fichero se busca por hash y el nombre es solo cosmetico.
func recortarExtension(s string) string {
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
		if strings.HasSuffix(strings.ToLower(s), ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}
