# Zerops x Echo

[Echo](https://echo.labstack.com/) is a high performance, extensible, minimalist Go web framework. This recipe aims to showcase basic Echo server-side rendered web app and how to integrate it with [Zerops](https://zerops.io), all through a simple file upload demo application.

![echo](https://github.com/zeropsio/recipe-shared-assets/blob/main/covers/svg/cover-echo.svg)

<br />

## Deploy on Zerops
You can either click the deploy button to deploy directly on Zerops, or manually copy the [import yaml](https://github.com/zeropsio/recipe-echo/blob/main/zerops-project-import.yml) to the import dialog in the Zerops app.

[![Deploy on Zerops](https://github.com/zeropsio/recipe-shared-assets/blob/main/deploy-button/green/deploy-button.svg)](https://app.zerops.io/recipe/echo)

<br/>

## Recipe features

- **Load balanced** Echo web app running on clean **Zerops Alpine** service
- Zerops **PostgreSQL 16** service as database
- Zerops **Object Storage** (S3 compatible) service file storage
- Zerops **KeyDB 6** service as Redis-compatible session storage
- Cloud-ready **database migration** and initial **data seeding**
- Utilization of Zerops built-in **environment and secret variables** system
- Logs accessible through Zerops GUI
- **[Mailpit](https://github.com/axllent/mailpit)** as **SMTP mock server**
- **[Adminer](https://www.adminer.org)** for **quick database management** tool
- Unlocked development experience:
    - Access to database and mail mock through Zerops project VPN (`zcli vpn up`)
    - Prepared `.env.dist` file (`cp .env.dist .env` and change ***** secrets found in Zerops GUI)
    - Run `npm install` to be able to re-build `tailwind.css` (`npm run build`)
    - Optional: install and use auto-reloading feature [air](https://github.com/air-verse/air)

<br/>

## Production vs. development

Base of the recipe is ready for production, the difference comes down to:

- Use highly available version of the PostgreSQL database (change `mode` from `NON_HA` to `HA` in recipe YAML, `db` service section)
- Use highly available version of the KeyDB store (change `mode` from `NON_HA` to `HA` in recipe YAML, `redis` service section)
- Use at least two containers for Echo service to achieve high reliability and resilience (add `minContainers: 2` in recipe YAML, `app` service section)
- Use production-ready third-party SMTP server instead of Mailpit (change `MAIL_` secret variables in recipe YAML `app` service)
- Disable public access to Adminer or remove it altogether (remove service `adminer` from recipe YAML)
- Secure cookies with `Domain` attribute set to your domain

<br/>
<br/>

## Changes made over the default installation

If you want to modify your existing Echo app to efficiently run on Zerops, these are the general steps we took:

- Use `os.Stdout` as logger output
- Disable HTTPS termination, since the app will run behind our automatic SSL load balancer proxy 

<br/>

Need help setting your project up? Join [Zerops Discord community](https://discord.com/invite/WDvCZ54).
