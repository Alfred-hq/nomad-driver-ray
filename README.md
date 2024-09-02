# Nomad Ray Driver Plugin
The Nomad Ray driver plugin is an experimental type of remote driver plugin. Whereas traditional Nomad driver plugins rely on running processes locally to the client, this experiment allows for the control of tasks at a remote destination. The driver is responsible for the lifecycle management of the remote process, as well as performing health checks and health decisions.

**Warning: this is an experimental feature and is therefore supplied without guarantees and is subject to change without warning. Do not run this in production.**

Nomad v1.1.0-beta1 or later is required.
