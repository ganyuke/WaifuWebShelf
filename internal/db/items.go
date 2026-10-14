package db

import (
	"database/sql"
	"errors"
	"fmt"
	"waifuwebshelf/internal/slug"
)

var ErrOwned = errors.New("This volume is already owned. Select Add another copy to continue.")

type ItemWrite struct {
	WishlistID                                       int64
	TripID, ReceiptLineID                            int64
	SeriesID, LineID                                 int64
	SeriesName, Kind, Label, Number, Name, MerchKind string
	Quantity, SortOrder                              int
	Merch, AnotherCopy                               bool
	Purchase                                         Purchase
	Cover                                            sql.NullString
}

// CreateItem commits the parents and item together, including the duplicate-copy check.
func (d *DB) CreateItem(in ItemWrite) (seriesID, lineID int64, seriesSlug string, err error) {
	tx, err := d.SQL.Begin()
	if err != nil {
		return 0, 0, "", err
	}
	defer tx.Rollback()
	seriesID, lineID = in.SeriesID, in.LineID
	unique := func(base, query string, args ...any) (string, error) {
		for i := 0; ; i++ {
			candidate := base
			if i > 0 {
				candidate = fmt.Sprintf("%s-%d", base, i+1)
			}
			var n int
			if e := tx.QueryRow(query, append(args, candidate)...).Scan(&n); e != nil {
				return "", e
			}
			if n == 0 {
				return candidate, nil
			}
		}
	}
	if seriesID == 0 {
		seriesSlug, err = unique(slug.Make(in.SeriesName), `SELECT COUNT(*) FROM series WHERE slug=?`)
		if err != nil {
			return
		}
		var res sql.Result
		res, err = tx.Exec(`INSERT INTO series(name,slug) VALUES(?,?)`, in.SeriesName, seriesSlug)
		if err != nil {
			return
		}
		seriesID, err = res.LastInsertId()
		if err != nil {
			return
		}
	} else {
		err = tx.QueryRow(`SELECT slug FROM series WHERE id=?`, seriesID).Scan(&seriesSlug)
		if err != nil {
			return
		}
	}
	if !in.Merch {
		if lineID == 0 {
			var ls string
			ls, err = unique(slug.ForBookLine(in.Kind, in.Label), `SELECT COUNT(*) FROM book_lines WHERE series_id=? AND slug=?`, seriesID)
			if err != nil {
				return
			}
			var res sql.Result
			res, err = tx.Exec(`INSERT INTO book_lines(series_id,kind,label,slug,default_currency,sort_order) VALUES(?,?,?,?,?,(SELECT COALESCE(MAX(sort_order),-1)+1 FROM book_lines WHERE series_id=?))`, seriesID, in.Kind, in.Label, ls, in.Purchase.Currency, seriesID)
			if err != nil {
				return
			}
			lineID, err = res.LastInsertId()
			if err != nil {
				return
			}
		} else {
			var parent int64
			err = tx.QueryRow(`SELECT series_id FROM book_lines WHERE id=?`, lineID).Scan(&parent)
			if err != nil {
				return
			}
			if parent != seriesID {
				err = errors.New("Format does not belong to this series.")
				return
			}
		}
		var n int
		err = tx.QueryRow(`SELECT COUNT(*) FROM volumes WHERE book_line_id=? AND volume_number=?`, lineID, in.Number).Scan(&n)
		if err != nil {
			return
		}
		if n > 0 && !in.AnotherCopy {
			err = ErrOwned
			return
		}
	}
	p := in.Purchase
	var trip, receiptLine sql.NullInt64
	if in.TripID != 0 {
		qty := 1
		if in.Merch {
			qty = in.Quantity
		}
		var tp Purchase
		tp, receiptLine, err = tripPurchase(tx, in.TripID, in.ReceiptLineID, qty)
		if err != nil {
			return
		}
		trip = sql.NullInt64{Int64: in.TripID, Valid: true}
		p.PurchasedAt, p.Store, p.Currency = tp.PurchasedAt, tp.Store, tp.Currency
		if receiptLine.Valid {
			p.PriceExTaxCents, p.PriceIncTaxCents = tp.PriceExTaxCents, nil
		}
	}
	if in.Merch {
		_, err = tx.Exec(`INSERT INTO merch(series_id,name,kind,quantity,purchased_at,store,msrp_cents,price_ex_tax_cents,price_inc_tax_cents,currency,image_path,notes,trip_id,receipt_line_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, seriesID, in.Name, in.MerchKind, in.Quantity, nullStr(p.PurchasedAt), p.Store, nullInt64Ptr(p.MSRPCents), nullInt64Ptr(p.PriceExTaxCents), nullInt64Ptr(p.PriceIncTaxCents), p.Currency, p.ImagePath, p.Notes, trip, receiptLine)
	} else {
		_, err = tx.Exec(`INSERT INTO volumes(book_line_id,volume_number,sort_order,purchased_at,store,msrp_cents,price_ex_tax_cents,price_inc_tax_cents,currency,image_path,notes,trip_id,receipt_line_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, lineID, in.Number, in.SortOrder, nullStr(p.PurchasedAt), p.Store, nullInt64Ptr(p.MSRPCents), nullInt64Ptr(p.PriceExTaxCents), nullInt64Ptr(p.PriceIncTaxCents), p.Currency, p.ImagePath, p.Notes, trip, receiptLine)
		if err == nil && in.Cover.Valid {
			_, err = tx.Exec(`INSERT INTO volume_covers(book_line_id,volume_number,image_path) VALUES(?,?,?) ON CONFLICT(book_line_id,volume_number) DO UPDATE SET image_path=excluded.image_path`, lineID, in.Number, in.Cover)
		}
	}
	if err != nil {
		return
	}
	if in.Merch && in.WishlistID > 0 {
		var result sql.Result
		result, err = tx.Exec(`UPDATE wishlist SET status='bought', updated_at=datetime('now') WHERE id=? AND series_id=? AND kind='merch' AND status='open'`, in.WishlistID, seriesID)
		if err != nil {
			return
		}
		var affected int64
		affected, err = result.RowsAffected()
		if err != nil {
			return
		}
		if affected != 1 {
			err = errors.New("wishlist item is no longer available")
			return
		}
	}
	err = tx.Commit()
	return
}
