---
title: werf.yaml
permalink: documentation/reference/werf_yaml.html
sidebar: documentation
toc: false
---

{% include documentation/reference/werf_yaml/table.html %}

## Имя проекта

Имя проекта должно быть уникальным в пределах группы проектов, собираемых на одном сборочном узле и развертываемых на один и тот же кластер Kubernetes (например, уникальным в пределах всех групп одного GitLab).

Имя проекта должно быть не более 50 символов, содержать только строчные буквы латинского алфавита, цифры и знак дефиса.

### Последствия смены имени проекта
 
**ВНИМАНИЕ**. Никогда не меняйте имя проекта в процессе работы, если вы не осознаете всех последствий.

Смена имени проекта приводит к следующим проблемам:

1. Инвалидация сборочного кэша. Все образы должны быть собраны повторно, а старые удалены из локального хранилища или Docker registry вручную.
2. Создание совершенно нового Helm-релиза. Смена имени проекта и повторное развертывание приложения приведет к созданию еще одного экземпляра, если вы уже развернули ваше приложение в кластере Kubernetes.

werf не поддерживает изменение имени проекта и все возникающие проблемы должны быть разрешены вручную.

## Выкат

### Имя релиза

werf позволяет определять пользовательский шаблон имени Helm-релиза, который используется во время [процесса деплоя]({{ "documentation/advanced/helm/basics.html#имя-релиза" | relative_url }}) для генерации имени релиза:

```yaml
project: PROJECT_NAME
configVersion: 1
deploy:
  helmRelease: TEMPLATE
  helmReleaseSlug: false
```

В качестве значения для `deploy.helmRelease` указывается Go-шаблон с разделителями `[[` и `]]`. Поддерживаются функции `project` и `env`. Значение шаблона имени релиза по умолчанию: `[[ project ]]-[[ env ]]`.

Можно комбинировать переменные доступные в Go-шаблоне с переменными окружения следующим образом:
{% raw %}
```yaml
deploy:
  helmRelease: >-
    [[ project ]]-{{ env "HELM_RELEASE_EXTRA" }}-[[ env ]]
```
{% endraw %}

`deploy.helmReleaseSlug` включает или отключает [слагификацию]({{ "documentation/advanced/helm/basics.html#слагификация-имени-релиза" | relative_url }}) имени Helm-релиза (включен по умолчанию).

### Namespace в Kubernetes 

werf позволяет определять пользовательский шаблон namespace в Kubernetes, который будет использоваться во время [процесса деплоя]({{ "documentation/advanced/helm/basics.html#namespace-в-kubernetes" | relative_url }}) для генерации имени namespace.

Пользовательский шаблон namespace Kubernetes определяется в секции мета-информации в файле `werf.yaml`:

```yaml
project: PROJECT_NAME
configVersion: 1
deploy:
  namespace: TEMPLATE
  namespaceSlug: true|false
```

В качестве значения для `deploy.namespace` указывается Go-шаблон с разделителями `[[` и `]]`. Поддерживаются функции `project` и `env`. Значение шаблона имени namespace по умолчанию: `[[ project ]]-[[ env ]]`.

`deploy.namespaceSlug` включает или отключает [слагификацию]({{ "documentation/advanced/helm/basics.html#слагификация-namespace-kubernetes" | relative_url }}) имени namespace Kubernetes. Включен по умолчанию.

## Очистка

## Конфигурация политик очистки

Конфигурация очистки состоит из набора политик, `keepPolicies`, по которым выполняется выборка значимых образов на основе истории git. Таким образом, в результате [очистки]({{ "documentation/advanced/cleanup.html#алгоритм-работы-очистки-по-истории-git" | relative_url }}) __неудовлетворяющие политикам образы удаляются__.

Каждая политика состоит из двух частей: 
- `references` определяет множество references, git-тегов или git-веток, которые будут использоваться при сканировании.
- `imagesPerReference` определяет лимит искомых образов для каждого reference из множества.

Любая политика должна быть связана с множеством git-тегов (`tag: string || /REGEXP/`) либо git-веток (`branch: string || /REGEXP/`). Можно указать определённое имя reference или задать специфичную группу, используя [синтаксис регулярных выражений golang](https://golang.org/pkg/regexp/syntax/#hdr-Syntax).

```yaml
tag: v1.1.1
tag: /^v.*$/
branch: master
branch: /^(master|production)$/
```

> При сканировании описанный набор git-веток будет искаться среди origin remote references, но при написании конфигурации префикс `origin/` в названии веток опускается  

Заданное множество references можно лимитировать, основываясь на времени создания git-тега или активности в git-ветке. Группа параметров `limit` позволяет писать гибкие и эффективные политики под различные workflow.

```yaml
- references:
    branch: /^features\/.*/
    limit:
      last: 10
      in: 168h
      operator: And
``` 

В примере описывается выборка из не более чем 10 последних веток с префиксом `features/` в имени, в которых была какая-либо активность за последнюю неделю.

- Параметр `last: int` позволяет выбирать последние `n` references из определённого в `branch`/`tag` множества.
- Параметр `in: duration string` (синтаксис доступен в [документации](https://golang.org/pkg/time/#ParseDuration)) позволяет выбирать git-теги, которые были созданы в указанный период, или git-ветки с активностью в рамках периода. Также для определённого множества `branch`/`tag`.
- Параметр `operator: And || Or` определяет какие references будут результатом политики, те которые удовлетворяют оба условия или любое из них (`And` по умолчанию).

По умолчанию при сканировании reference количество искомых образов не ограничено, но поведение может настраиваться группой параметров `imagesPerReference`:

```yaml
imagesPerReference:
  last: int
  in: duration string
  operator: And || Or
```

- Параметр `last: int` определяет количество искомых образов для каждого reference. По умолчанию количество не ограничено (`-1`).
- Параметр `in: duration string` (синтаксис доступен в [документации](https://golang.org/pkg/time/#ParseDuration)) определяет период, в рамках которого необходимо выполнять поиск образов.
- Параметр `operator: And || Or` определяет какие образы сохранятся после применения политики, те которые удовлетворяют оба условия или любое из них (`And` по умолчанию)

> Для git-тегов проверяется только HEAD-коммит и значение `last` >1 не имеет никакого смысла, является невалидным

При описании группы политик необходимо идти от общего к частному. Другими словами, `imagesPerReference` для конкретного reference будет соответствовать последней политике, под которую он подпадает:

```yaml
- references:
    branch: /.*/
  imagesPerReference:
    last: 1
- references:
    branch: master
  imagesPerReference:
    last: 5
```

В данном случае, для reference _master_ справедливы обе политики и при сканировании ветки `last` будет равен 5.

### Политики по умолчанию

В случае, если в `werf.yaml` отсутствуют пользовательские политики очистки, используются политики по умолчанию, соответствующие следующей конфигурации:

```yaml
cleanup:
  keepPolicies:
  - references:
      tag: /.*/
      limit:
        last: 10
  - references:
      branch: /.*/
      limit:
        last: 10
        in: 168h
        operator: And
    imagesPerReference:
      last: 2
      in: 168h
      operator: And
  - references:  
      branch: /^(master|staging|production)$/
    imagesPerReference:
      last: 10
``` 

Разберём каждую политику по отдельности:

1. Сохранять образ для 10 последних тегов (по дате создания).
2. Сохранять по не более чем два образа, опубликованных за последнюю неделю, для не более 10 веток с активностью за последнюю неделю. 
3. Сохранять по 10 образов для веток master, staging и production. 

## Секция image

Образы описываются с помощью директивы _image_: `image: string`, с которой начинается описание образа в конфигурации.

```yaml
image: frontend
```

Если в файле конфигурации описывается только один образ, то он может быть безымянным:

```yaml
image: ~
```

Если в файле конфигурации описывается более одного образа, то **каждый образ** должен иметь собственное имя:

```yaml
image: frontend
...
---
image: backend
...
```

Образ может иметь несколько имен, указываемых в виде YAML-списка (это эквивалентно описанию нескольких одинаковых образов с разными именами):

```yaml
image: [main-front,main-back]
```

Имя образа требуется при использовании в helm-шаблонах, а также при запуске команд для определённого образа, описанного в `werf.yaml`.

### Сборщик Dockerfile

Сборка образа с использованием имеющегося Dockerfile — самый простой путь начать использовать werf в существующем проекте. Ниже приведен пример минимального файла `werf.yaml`, описывающего образ `example` проекта:

```yaml
project: my-project
configVersion: 1
---
image: example
dockerfile: Dockerfile
```

Также, вы можете описывать несколько образов из одного и того же Dockerfile:

```yaml
image: backend
dockerfile: Dockerfile
target: backend
---
image: frontend
dockerfile: Dockerfile
target: frontend
```

И конечно, вы можете описывать образы, основанные на разных Dockerfile:

```yaml
image: backend
dockerfile: dockerfiles/DockerfileBackend
---
image: frontend
dockerfile: dockerfiles/DockerfileFrontend
```

### Stapel сборщик

Альтернативой создания образов с помощью Dockerfiles является werf stapel builder, который позволяет существенно ускорить сборку за счёт тесной интеграции с Git.
