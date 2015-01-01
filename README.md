gopee
-----

HTTP Web Proxy - Simple proxy service to access blocked websites

Demo

https://gopee.herokuapp.com/

or

http://go-gopee.rhcloud.com/

Deploying This Application
==========================

- Fork this repo

- Update .godir to match your forked repo url

#### Deploy to [heroku](https://www.heroku.com/)

- Download and Setup Heroku Toolbelt https://toolbelt.heroku.com/

- Clone the forked repo locally and `cd gopee`

- Create a new Heroku app, telling it to use the [Go Heroku Buildpack](https://github.com/kr/heroku-buildpack-go) to build your Go code

```sh
heroku create -b https://github.com/kr/heroku-buildpack-go.git
```

```
Creating polar-harbor-5778... done, stack is cedar-14
BUILDPACK_URL=https://github.com/kr/heroku-buildpack-go.git
https://polar-harbor-5778.herokuapp.com/ | https://git.heroku.com/polar-harbor-5778.git
Git remote heroku added
```

- Push the code to Heroku

```sh
git push heroku master
```

#### Deploy to [OpenShift by Red Hat](https://www.openshift.com/)

- Download and Setup OpenShift Client Tools (*rhc*) https://developers.openshift.com/en/managing-client-tools.html

- Create a new OpenShift application, telling it to use the [OpenShift Go Cartridge](https://github.com/smarterclayton/openshift-go-cart) to build your Go code and your forked github repo for Source

```sh
rhc app create myproxy https://cartreflect-claytondev.rhcloud.com/reflect?github=smarterclayton/openshift-go-cart --from-code <forked-github-repo-url>
```

```
Your application 'myproxy' is now available.
  URL:        http://myproxy-gopee.rhcloud.com/
  SSH to:     <hash>@myproxy-gopee.rhcloud.com
  Git remote: ssh://<hash>@myproxy-gopee.rhcloud.com/~/git/myproxy.git/
```

- Open the above URL in your browser

- Login to your OpenShift console and you should see an application created

## Errata

- Doesn't support anything which requires sessions / cookies / logging in, yet

- Most AJAX requests don't work since the URLs are not rewritten

## Why ?

To learn Golang
 
## Credits

[@Omie](https://github.com/Omie) for pushing me to learn Go and introducing me to Heroku and OpenShift