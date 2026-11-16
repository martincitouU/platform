# Bitácora de aprendizajes

## Fase 1 · La VM manual

Durante la creación de la cuenta en aws es importante gestionar los roles IAM y la habilitación de MFA para la cuenta.

'aws login' permite conectarse con la cuenta de aws por terminal en la máquina, 'aws sts get-caller-identity' permite comprobar el correcto logeo con la cuenta.

¿Qué se necesita para que la instancia EC2 sea alcanzable desde fuera?
Primero que todo una ip pública, un internet gateway (enlace entre VPC e internet). Una Route table de la subnet con la ruta `0.0.0.0/0 → IGW`.
 Una subnet con esa ruta se llama subnet pública. De igual manera se requiere un Security group, que permita el tráfico inicial entrante al puerto 22 (por default no se permite tráfico).

 Cabe destacar que aws trae los 3 primeros componentes ya armados, para levantar una infraestructura con terraform estos deben ser configurados maualmente.

Luego de crear la instancia en EC2 con las configuraciones deseadas se debe realizar la primera conexión a través de ssh, el usuario por defecto es ubuntu (port 22) y la máquina permite todo el tráfico entrante y saliente. Desde el panel de AWS se agrega un nuevo grupo de seguridad, donde se agrega una regla de entrada que sólo permite la ip de la máquina actual, para evitar conexiones no deseadas. 

Una vez dentro del VM, se crea un archivo de configuración en la ruta '/etc/ssh/sshd_config.d/ con los siguientes parámetros: 
PermitRootLogin no
PasswordAuthentication no
PubkeyAuthentication yes
AllowUsers martin ubuntu

Esto permite a solo 2 usuarios conectarse y poder autenticarse sólo con llaves y no con contraseñas, para de esta manera endurecer y hacer más estrictas las vías de conexión al VM.

Dentro de la VM también se configura ufw, un firewall a nivel de máquina. primero se comprueba que esté desactivado con 'sudo ufw status verbose', luego permitir la entrada y salida de tráfico por defecto con 'sudo ufw default deny incoming y sudo ufw default allow outgoing' y sumado a eso se agrega de manera explícita a ssh para entrada con 'sudo ufw allow OpenSSH'.

Finalmente se confirma que todo esté correcto con 'sudo ufw show added' y luego se activa con 'sudo ufw enable'. Esta combinación de técnicas permite tener diferentes capas de seguridad a nivel tanto de cuenta AWS como de máquina.

El servicio "unattended-upgrades" permite al sistema aplicar parches de seguridad automáticamente cada día, normalmente vulnerabilidades publicadas por oficiales.

## Fase 1 · Contenedores y CI/CD

Las imágenes se construyen en dos etapas (multi-stage): una con todo Go para compilar y otra final distroless, sin shell ni gestor de paquetes, que solo contiene el binario. Copiar primero `go.mod` y `go.sum` aprovecha la caché de capas: si solo cambia el código, las dependencias no se vuelven a descargar.

En GitHub Actions el orden de los jobs no lo decide su posición en el archivo sino `needs`. Un job sin `needs` arranca en paralelo desde el principio.

Las actions se fijan por SHA y no por tag (`@v4`), porque un tag se puede mover a otro código. El caso real fue el compromiso de trivy-action en marzo de 2026 (CVE-2026-33634). Dependabot mantiene esos SHA actualizados con PRs semanales.

Cada imagen se etiqueta con el SHA del commit, así se sabe exactamente qué código corre en producción y el rollback consiste en volver a levantar el SHA anterior.

El runner self-hosted no recibe conexiones: abre una conexión saliente hacia GitHub y pregunta por trabajo (long polling). Por eso funciona aunque el Security Group solo permita entrar por el puerto 22. El job pide etiquetas (`[self-hosted, prod]`), no una máquina: GitHub se lo da a cualquier runner online que las tenga. Si dos runners con la misma etiqueta están activos, cualquiera puede tomar el deploy.

## Fase 2 · Terraform

Terraform lee todos los `.tf` de una carpeta juntos y decide el orden a partir de las referencias entre recursos (el grafo de dependencias). Al destruir recorre el grafo al revés. Cuando una dependencia no aparece en el código (la Elastic IP necesita el Internet Gateway pero no lo menciona) se declara con `depends_on`.

Un `data source` consulta algo que ya existe en AWS sin administrarlo (las zonas de disponibilidad, la AMI más reciente de Canonical). Con `most_recent = true` la AMI cambia cada semana y Terraform querría reemplazar la VM; `lifecycle { ignore_changes = [ami] }` evita eso.

`terraform plan -out` + `terraform apply <plan>` garantiza que se aplica exactamente lo que se revisó.

Las validaciones en las variables frenan errores antes de tocar AWS: `my_ip_cidr` exige una sola IP (`/32`), así es imposible abrir el SSH al mundo por un error de tipeo.

`for_each` crea una instancia de un recurso por cada elemento de un mapa (`web["https"]`, `web["http"]`); `locals` da nombre a valores internos.

El state es la memoria de Terraform. Vivía solo en mi Mac: si se perdía, la infraestructura quedaba huérfana. Ahora vive en S3 con versionado, cifrado, acceso solo por HTTPS y bloqueo nativo (`use_lockfile`, desde Terraform 1.10, sin DynamoDB). El bucket se crea con un stack aparte (bootstrap) para resolver el huevo y la gallina. El bloque `backend` no acepta variables porque se lee antes que todo lo demás. `terraform init -migrate-state` mueve el state sin tocar ningún recurso.

## Fase 2 · Ansible

Terraform construye la casa (habla con la API de AWS) y Ansible la amuebla (entra por SSH). Ansible no tiene state: en cada corrida revisa la máquina real.

Cada tarea describe un estado deseado (`state: present`), no una acción. Por eso es idempotente: la segunda corrida da `changed=0`. Pero `changed=0` significa que la máquina está como dice el código, no como uno quiere: si el cambio no está guardado en el archivo, Ansible no lo aplica.

El módulo `command` no es idempotente por sí solo; `creates:` le dice que se salte la tarea si un archivo ya existe.

Los tests de Jinja como `is exists` se evalúan en la máquina desde donde se lanza Ansible, no en la VM. Para revisar un archivo remoto se usa el módulo `stat` con `register`.

`no_log: true` oculta todo el resultado de una tarea, incluido el error. Para ver el mensaje sin exponer el token se guarda el resultado con `register` y se muestra solo stdout/stderr.

En `ufw` primero se permite SSH y después se activa el firewall; al revés uno queda fuera de su propia VM.

## Fase 2 · La migración

Antes de migrar se compara la VM vieja con la nueva para detectar drift (lo que se hizo a mano y no está en el código).

El orden importa: congelar los despliegues (detener el runner viejo), respaldar con `pg_dump -Fc`, verificar el respaldo con `pg_restore --list`, levantar solo la base en la VM nueva, restaurar, comparar conteos, registrar el runner nuevo y recién ahí desplegar. Si la API arranca antes de restaurar, sus migraciones crean tablas vacías que chocan con el respaldo.

## Diagnóstico

Un timeout significa que algo bota los paquetes en silencio (Security Group, NACL o `ufw` con política deny). Un "connection refused" significa que la máquina responde pero nada escucha en ese puerto.

El Security Group es stateful: las respuestas a conexiones que inició la VM entran sin mirar las reglas de entrada. La NACL es stateless y necesita permitir los puertos efímeros.

Se diagnostica por capas: red local, máquina encendida, firewall de AWS, firewall de la VM.

`terraform state show` muestra lo último guardado, no la realidad; `plan` sí consulta AWS. Un `plan` sin cambios garantiza que AWS coincide con lo que se le pasó a Terraform, no que lo que se le pasó sea correcto (una variable de entorno con una IP vieja).

Mi red sale a internet por dos IPs públicas y reparte las conexiones entre ellas: con un SG `/32` algunos intentos de SSH entraban y otros no. Un problema intermitente casi siempre tiene dos caminos.

La cuenta está dentro de una organización con SCP: un deny explícito le gana a cualquier allow (por eso OIDC no fue posible y se usó GHCR). La CLI de AWS usa su propia región por defecto, distinta de la de Terraform, y las SCP bloquean las regiones no permitidas.

## Seguridad de la infraestructura

`trivy config` escanea Terraform, Dockerfile y Compose en cada PR. Cada hallazgo se corrige o se acepta con una razón escrita junto al código (`#trivy:ignore:AWS-0104`), nunca se silencia sin explicar. La salida a internet se redujo de todos los puertos a TCP 80 y 443; el destino `0.0.0.0/0` se mantiene porque GitHub, GHCR y apt usan IPs cambiantes. El DNS y el NTP de Amazon dentro de la VPC no pasan por el Security Group.

IMDSv2 obliga a pedir un token antes de leer los metadatos de la instancia, lo que bloquea ataques SSRF que intentan robar credenciales.
