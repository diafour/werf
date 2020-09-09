---
title: Базовые настройки
sidebar: applications_guide
guide_code: gitlab_nodejs
permalink: documentation/guides/applications_guide/gitlab_nodejs/020_basic.html
layout: guide
toc: false
---

В этой главе мы возьмём приложение, которое будет выводить сообщение «Hello world!» по HTTP, и опубликуем его в Kubernetes с помощью werf. Сперва разберёмся со сборкой и добьёмся того, чтобы образ оказался в Registry, затем — разберёмся с деплоем собранного приложения в Kubernetes, после чего, наконец, организуем CI/CD-процесс силами GitLab CI.

Если у вас мало опыта с описанием объектов Kubernetes — учтите, что это может занять у вас больше времени, чем организация сборки и CI-процесса с помощью werf. Это нормально, и мы постарались облегчить данный процесс.

Приложение будет состоять из одного Docker-образа, собранного с помощью werf. В этом образе будет работать один основной процесс, который запустит Node. Управлять маршрутизацией запросов к приложению будет Ingress в Kubernetes кластере. Мы реализуем два стенда: [production](https://ru.werf.io/documentation/reference/ci_cd_workflows_overview.html#production) и [staging](https://ru.werf.io/documentation/reference/ci_cd_workflows_overview.html#staging).

<div>
    <a href="020_basic/10_build.html" class="nav-btn">Далее: Сборка</a>
</div>
