// Pantalla pendiente de construir.
//
// El esqueleto se deja para que el menú y las rutas existan y compilen: sin
// esto, la entrada del menú lleva a una pantalla en blanco, que es peor que
// una que dice lo que falta.
export function Mediateca() {
  return (
    <>
      <div className="nota-previa">
        Las imágenes se gestionan por producto: abre cualquiera desde <strong>Productos</strong>
        {' '}para subir, quitar o elegir portada. La búsqueda masiva de arriba cubre
        todo el catálogo de una vez.
      </div>
    </>
  )
}
