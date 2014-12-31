gopee
-----

HTTP Web Proxy - Simple proxy service to access blocked websites

## Deploy to heroku

#### Create a new Heroku app, telling it to use the [Go Heroku Buildpack](https://github.com/kr/heroku-buildpack-go) to build your Go code:

```sh
heroku create -b https://github.com/kr/heroku-buildpack-go.git
```

```
Creating polar-harbor-5778... done, stack is cedar-14
BUILDPACK_URL=https://github.com/kr/heroku-buildpack-go.git
https://polar-harbor-5778.herokuapp.com/ | https://git.heroku.com/polar-harbor-5778.git
Git remote heroku added
```

#### Push the code to heroku
```sh
git push heroku master
```

## Credits

[@Omie](https://github.com/Omie) for pushing me to learn Go