This API returns the yield or price of a given pre-loaded bond in bonds.json
The endpoint checks if the requested bond is zerocoupon.
 If bonds is index adjusted, it will look for the coefficientes of IssueDate, settlementDate and calculate a ratio. Works only with CER (http://www.bcra.gob.ar/PublicacionesEstadisticas/Principales_variables_datos.asp?serie=3540&detalle=CER%A0(Base%202.2.2002=1))


 The coefficients are stored in a sqlite3 database stored locally.
 There's a call in the getCER() that uses a python script to download and populate a sqlite database with the last series. It is called every time the API starts or after 24 hours from a cron job.
 Python should be installed on the system. 
 The two source files needed are included in the repo for you compiling convenience.

The script implements Gin-gonic to set up an API and the following endpoints:

1.- yield
2.- price
3.- schedule
4.- upload
5.- bonds
6.- apr

1.- yield 

Value: (float64) Yield: Returns ytm of the bond given its price and cashflow. Works with indexed and non-indexed bonds.
      (float64) MDuration: Returns modified duration of the bond.
      (int) AccrualDays: Accrual days since last interest payment.
      (float64) CurrentCoupon: actual coupon based on date.
      (float64) Residual: Outstanding principal amount.
      (float64) AccruedInterest: Accrued interest since last coupon.
      (float64) TechnicalValue: technical value.
      (float64) Parity: parity of the bond.
      (string) LastCoupon: date of last coupon.
      (string) LasAmort: date of last amortization (payment of principal)
      (float64): CoefUsed: coefficient used for settlement date. It takes the offset of the bond (how many working days to look back) and, based on ExtendedIndex value during API call. 
      (float64): CoefIssue: coefficient of the issuing date. Takes offset of the bond into account.
      (string): CoefFechaCalculo: date of the coefficient used for settlement date.
      (string): Maturity: of the bond.


Params:
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format. 
  price: (float64) required price of the referred bond
  initialFee: (float64) fee to charge on the beginning of the cashflow. Usually broker fee. Could be zero.
  endingFee: (float64) fee to charge on the end of the cashflow. Usually broker fee. Could be zero.
  extendIndex: (float64) rate (in anual terms) to use to extend coefficient in case it ends before settlement date.
  
 2.- price
 
 Value: (float64) Price: price of the bond given its return and cashflow.
        (float64) MDuration: Returns modified duration of the bond.
        (int) AccrualDays: Accrual days since last interest payment.
        (float64) CurrentCoupon: actual coupon based on date.
        (float64) Residual: Outstanding principal amount.
        (float64) AccruedInterest: Accrued interest since last coupon.
        (float64) TechnicalValue: technical value.
        (float64) Parity: parity of the bond.
        (string) LastCoupon: date of last coupon.
        (string) LasAmort: date of last amortization (payment of principal)
        (float64): CoefUsed: coefficient used for settlement date. It takes the offset of the bond (how many working days to look back) and, based on ExtendedIndex value during API call. 
        (float64): CoefIssue: coefficient of the issuing date. Takes offset of the bond into account.
        (string): CoefFechaCalculo: date of the coefficient used for settlement date.
        (string): Maturity: of the bond.
 
 Params:
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format. 
  rate: (float64) required rate for the given bond
  initialFee: (float64) fee to charge on the beginning of the cashflow. Usually broker fee. Could be zero.
  endingFee: (float64) fee to charge on the end of the cashflow. Usually broker fee. Could be zero.
  extendIndex: (float64) rate (in anual terms) to use to extend coefficient in case it ends before settlement date.
  
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

 Idem 1 but returns the APR instead of ytm. Works only with zero coupon bonds. The endpoint checks if the requested bond is zerocoupon.
 If bonds is index adjusted, it will look for the coefficientes of IssueDate, settlementDate and calculate a ratio. Works only with CER (http://www.bcra.gob.ar/PublicacionesEstadisticas/Principales_variables_datos.asp?serie=3540&detalle=CER%A0(Base%202.2.2002=1))

 Value: (float64) Returns APR of the bond given its price and cashflow. 
        (float64) Returns modified duration of the bond.
        (int) AccrualDays: Accrual days since last interest payment.
        (float64) CurrentCoupon: actual coupon based on date.
        (float64) Residual: Outstanding principal amount.
        (float64) AccruedInterest: Accrued interest since last coupon.
        (float64) TechnicalValue: technical value.
        (float64) Parity: parity of the bond.
        (string) LastCoupon: N/A for this type of bonds.
        (float64): CoefUsed: coefficient used for settlement date. It takes the offset of the bond (how many working days to look back) and, based on ExtendedIndex value during API call. 
        (float64): CoefIssue: coefficient of the issuing date. Takes offset of the bond into account.
        (string): CoefFechaCalculo: date of the coefficient used for settlement date.
        (string): Maturity: of the bond.

Params:
  ticker: (string) ticker of the pre-loaded bond.
  settlementDate: (string) in `"2006-01-02"` format. 
  price: (float64) required price of the referred bond
  initialFee: (float64) fee to charge on the beginning of the cashflow. Usually broker fee. Could be zero.
  endingFee: (float64) fee to charge on the end of the cashflow. Usually broker fee. Could be zero.
  extendIndex: (float64) rate (in anual terms) to use to extend coefficient in case it ends before settlement date.
  
 
