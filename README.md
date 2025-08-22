# Module weatherviz 

This module takes in weather data and visualizes it. 

## Model vijayvuyyuru:weatherviz:weatherviz

This module takes in weather data and visualizes it. 

### Configuration
The following attribute template can be used to configure this model:

```json
{
"led-component": <string>,
"weather-sensor": <string>
}
```

#### Attributes

The following attributes are available for this model:

| Name          | Type   | Inclusion | Description                |
|---------------|--------|-----------|----------------------------|
| `led-component` | string  | Required  | LED module to send DoCommands to. |
| `weather-sensor` | string | Required  | Weather sensor that is input to visualize. |

#### Example Configuration

```json
{
  "led-component": "yabbadabba",
  "weather-sensor": "doo"
}
```

### DoCommand

Only command is {visualize: ""}

#### Example DoCommand

```json
{
  "visualize": ""
}
```
