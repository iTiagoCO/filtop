# Filebeat Monitor (filTop)

Una herramienta interactiva de línea de comandos para monitorear el rendimiento de Filebeat en tiempo real, incluyendo uso de CPU, memoria, eventos procesados y estado de harvesters.

## 🚀 Características
- Monitoreo en tiempo real de métricas clave de Filebeat.
- Visualización de harvesters, inputs y módulos activos.
- Configurable por host, puerto e intervalo de actualización.

## 📦 Requisitos
- Go 1.20 o superior
- Filebeat con la API de stats habilitada (generalmente en `localhost:5066`)

## 🔧 Instalación
Clona el repositorio y compila el binario:
```bash
git clone https://github.com/iTiagoCO/filtop.git
cd filtop
go mod tidy
go build -o filtop filtop.go