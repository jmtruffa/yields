This API returns the yield or price of a given pre-loaded bond in bonds.json

The script implements Gin-gonic to set up an API and the following endpoints:

1.- yield
2.- price
3.- schedule
4.- upload
5.- bonds
6.- apr

1.- yield 

Value: (float64) Return yield of the bond given its price and cashflow
      (float64) Return modified duration of the bond.

Params:
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format. 
  price: (float64) required price of the referred bond
  initialFee: (float64) fee to charge on the beginning of the cashflow. Usually broker fee. Could be zero.
  endingFee: (float64) fee to charge on the end of the cashflow. Usually broker fee. Could be zero.
  
 2.- price
 
 Value: (float64) Price of the bond given its return and cashflow.
        (float64) Return modified duration of the bond.
 
 Params:
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format. 
  rate: (float64) required rate for the given bond
  initialFee: (float64) fee to charge on the beginning of the cashflow. Usually broker fee. Could be zero.
  endingFee: (float64) fee to charge on the end of the cashflow. Usually broker fee. Could be zero.
  
 3.- schedule
 
 Value: (json) Schedule of payments of the given bond from the settlement date.
 
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format.
 
4.- upload

Value: (json) Message and ID of the uploaded bond.

This API implements these functions from /alpeb/go-finance/:

- ScheduledInternalRateOfReturn
- ScheduledNetPresentValue
- dScheduledNetPresentValue
- minMaxSlice
- newton
 
 5.- bonds
 
 Value: (json) The list of bonds available in the API
 
 This endpoint does not require any params.

 6.- apr

 Idem 1 but returns in APR instead of effective yield. Works only with zero coupon bonds. The endpoint checks if the requested bond is zerocoupon.
 If bonds is index adjusted, it will look for the coefficientes of IssueDate, settlementDate and calculate a ratio. Works only with CER (http://www.bcra.gob.ar/PublicacionesEstadisticas/Principales_variables_datos.asp?serie=3540&detalle=CER%A0(Base%202.2.2002=1))

 Value: (float64) Return APR of the bond given its price and cashflow
      (float64) Return modified duration of the bond.

Params:
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format. 
  price: (float64) required price of the referred bond
  initialFee: (float64) fee to charge on the beginning of the cashflow. Usually broker fee. Could be zero.
  endingFee: (float64) fee to charge on the end of the cashflow. Usually broker fee. Could be zero.
  
 
