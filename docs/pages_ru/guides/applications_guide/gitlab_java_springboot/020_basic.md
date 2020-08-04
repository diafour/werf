---
title: Базовые настройки
sidebar: applications_guide
guide_code: gitlab_java_springboot
permalink: documentation/guides/applications_guide/gitlab_java_springboot/020_basic.html
layout: guide
toc: false
---

В этой главе мы возьмём приложение, которое будет выводить сообщение "hello world" по http и опубликуем его в kubernetes с помощью Werf. Сперва мы разберёмся со сборкой и добьёмся того, чтобы образ оказался в Registry, затем — разберёмся с деплоем собранного приложения в Kubernetes, и, наконец, организуем CI/CD-процесс силами Gitlab CI. 

Наше приложение будет состоять из одного docker образа собранного с помощью werf. В этом образе будет работать один основной процесс, который запустит java, исполняющий собранный jar отдающий hello world по http. Управлять маршрутизацией запросов к приложению будет Ingress в Kubernetes кластере. Мы реализуем два стенда: [production](https://ru.werf.io/documentation/reference/ci_cd_workflows_overview.html#production) и [staging](https://ru.werf.io/documentation/reference/ci_cd_workflows_overview.html#staging).

<div>
    <a href="020_basic/10_build.html" class="nav-btn">Далее: Сборка</a>
</div>
