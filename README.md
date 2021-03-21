# Lucrum

A Coinbase cryptocurrency trading bot

### Setup

It's probably helpful to read through the list first before trying any of the actions in it. 
Feel free to not use Docker if you would like to set up a database a different way.

1. Create a coinbase account (obviously).
1. Create a trading portfolio on [Coinbase Pro](https://pro.coinbase.com/) and the [Coinbase Pro 
   Sandbox](https://public.sandbox.pro.coinbase.com/)
1. [Create API keys](https://help.coinbase.com/en/pro/other-topics/api/how-do-i-create-an-api-key-for-coinbase-pro) for each service (production/sandbox)
1. Copy over the configuration files
   1. Copy the `example-config.toml` over to `config.toml`
      1. Store the API keys in the appropriate sections of `config.toml`
      1. Set up the configuration pointing to where you plan to host your database server(s)
      1. Make sure to set a sufficient password
   
   #### If using Docker
   1. Copy the `.env-template` over to `.env`
      1. Use the same database password  as the one in config.toml
   1. Tweak the port configuration in `docker-compose.yml` if needed
   1. `docker-compose up -d`

1. `$ go install`

### Running Lucrum
Getting a list of command line args available
```
$ lucrum --help
Usage: lucrum

Flags:
-h, --help                                 Show context-sensitive help.
-c, --config=STRING                        Configuration file
-b, --background                           Run the bot in the background
--background-ws                            Run the websocket handler in the background
-d, --drop-tables                          Wipes the tables in the db to get a fresh start
-r, --historic-rates=HISTORIC-RATES,...    CSV files for rates to read in
-s, --sandbox                              Run the box in Sandbox mode
-u, --un-sandbox                           Run the box without Sandbox mode
-w, --ws                                   Explicitly run only the websocket in the foreground
-v, --verbose                              Increase verbosity level
   ```

Running normally will invoke the bot using the configuration file
```
$ lucrum
2021/03/20 23:16:10 Connection to db succeeded!
2021/03/20 23:16:10 RUNNING IN SANDBOX MODE
```
If the configuration said not to use sandbox mode or the `-u` flag was passed
```
$ lucrum -u
2021/03/21 00:04:38 Connection to db succeeded!
2021/03/21 00:04:39 YOU ARE NOT IN A SANDBOX! ARE YOU SURE YOU WANT TO CONTINUE? (Y/n)
```
Running explicitly in websocket mode. Note: the websocket currently does not care if you are in 
production or sandbox, so there is no prompt if in production mode. This is because there 
currently is nothing to potentially lose by screwing up in production
```
$ lucrum -w
2021/03/20 23:16:10 Connection to db succeeded!
2021/03/20 23:16:10 RUNNING IN SANDBOX MODE
```

### Planned Features for version 1

- [X] Both Sandbox and Production mode on Coinbase
- [X] Require user input for production mode to work (help mitigate screwing up)
- [X] Configuration file
- [X] Argument Parsing
- [X] Grabbing historical data from Coinbase
- [X] Making use of the websocket
- [X] Daemonizing both the main bot and the websocket separately from one another
- [X] Signal interrupt handling
- [X] Simple Logging
- [ ] Robust logging
- [ ] Actually doing trading
- [ ] Possibly Random Forest / Genetic algorithm approach to the trading algorithms
- [ ] Parameters to the algorithms are configurable
- [ ] Logging all transactions and keeping a full order book of all user actions
- [ ] Explicitly logging all decisions made to analyze the effect of these decisions in the 
  future analysis
- [ ] Collecting extensive statistics on order book data for a candle
    - [ ] Separate repository, but sandbox for backtesting on collected data
    - [ ] Bot functionality to run on this backtest and measure efficacy

### Planned Features for version 2
- [ ] Generic library for doing trading. Not platform specific, just requires drivers to be provided for each platform 
- [ ] Web front end
   - [ ] User defines which types of algorithms run
  - [ ] User defines their own 
     algorithms
  - [ ] Tools for analyzing results of the bot live and past data
