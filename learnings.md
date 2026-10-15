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

Finalmente se confirma que todo esté correcto con 'sudo ufw show added' y luego se activa con 'sudo ufw enable'.

Esta combinación de técnicas permite tener diferentes capas de seguridad a nivel tanto de cuenta AWS como de máquina.